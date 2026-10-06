package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"svolo.local/core/internal/browser"
	"svolo.local/core/internal/store"
)

type recording struct {
	mu                 sync.Mutex
	id, sid, dir, mode string
	interval           int
	cancel             context.CancelFunc
	done               chan struct{}
	frames             int
	bytes              int64
	err                string
	timestamps         []time.Time
}

func (s *Server) record(ctx context.Context, sid, actor string, a map[string]any) (any, error) {
	action := arg(a, "action")
	if action == "start" {
		mode := arg(a, "mode")
		if mode == "" {
			mode = "continuous"
		}
		if mode != "continuous" && mode != "steps" {
			return nil, errors.New("mode must be continuous or steps")
		}
		interval := int(num(a, "intervalMs", 500))
		if interval < 100 || interval > 10000 {
			return nil, errors.New("intervalMs must be 100..10000")
		}
		s.mu.Lock()
		if s.recordings[sid] != nil {
			s.mu.Unlock()
			return nil, errors.New("a recording already exists; stop it first")
		}
		rc, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
		r := &recording{id: store.ID(), sid: sid, mode: mode, interval: interval, cancel: cancel, done: make(chan struct{})}
		r.dir = filepath.Join(s.Store.Root, "recordings", sid, r.id)
		if err := os.MkdirAll(r.dir, 0700); err != nil {
			cancel()
			s.mu.Unlock()
			return nil, err
		}
		s.recordings[sid] = r
		s.mu.Unlock()
		go func() {
			defer close(r.done)
			s.capture(rc, r)
			if r.mode == "steps" {
				<-rc.Done()
				return
			}
			tick := time.NewTicker(time.Duration(r.interval) * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-rc.Done():
					return
				case <-tick.C:
					s.capture(rc, r)
				}
			}
		}()
		return map[string]any{"id": r.id, "mode": r.mode, "intervalMs": interval, "capture": "sampled-page-pixels", "maxFrames": 1200, "maxDurationSeconds": 600}, nil
	}
	if action != "stop" && action != "status" {
		return nil, errors.New("action must be start, stop or status")
	}
	s.mu.Lock()
	r := s.recordings[sid]
	s.mu.Unlock()
	if r == nil {
		return nil, errors.New("no recording for session")
	}
	if action == "status" {
		r.mu.Lock()
		defer r.mu.Unlock()
		return map[string]any{"id": r.id, "mode": r.mode, "frames": r.frames, "bytes": r.bytes, "error": r.err}, nil
	}
	r.cancel()
	<-r.done
	s.mu.Lock()
	delete(s.recordings, sid)
	s.mu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	manifest := map[string]any{"id": r.id, "session": sid, "mode": r.mode, "capture": "sampled page screenshots; not lossless full-desktop video", "intervalMs": r.interval, "frames": r.frames, "timestamps": r.timestamps, "captureError": r.err}
	_ = s.Store.Write("recording-"+r.id, manifest)
	if r.frames == 0 {
		return manifest, errors.New("no recording frames captured")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return manifest, errors.New("ffmpeg not found; PNG frames and manifest are retained, no video falsely reported")
	}
	out := filepath.Join(r.dir, "recording.mp4")
	encCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	fps := fmt.Sprintf("%.6f", 1000/float64(r.interval))
	if r.mode == "steps" {
		fps = "1"
	}
	cmd := exec.CommandContext(encCtx, ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-framerate", fps, "-i", filepath.Join(r.dir, "frame-%06d.png"), "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", out)
	if data, err := cmd.CombinedOutput(); err != nil {
		return manifest, fmt.Errorf("encoding failed (%s); PNG frames retained: %w", string(data), err)
	}
	st, err := os.Stat(out)
	if err != nil {
		return manifest, err
	}
	if st.Size() > 64<<20 {
		return manifest, errors.New("encoded video exceeds artifact budget; recording remains on disk")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return manifest, err
	}
	artifact, err := browser.SaveArtifact(s.Store.Root, sid, "recording.mp4", data)
	if err != nil {
		return manifest, err
	}
	manifest["artifact"] = artifact
	_ = s.Store.Write("recording-"+r.id, manifest)
	// Immutable video+manifest are retained. PNG capture cache is removed only
	// after successful encoding and artifact hashing.
	_ = os.RemoveAll(r.dir)
	return manifest, nil
}
func (s *Server) capture(ctx context.Context, r *recording) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frames >= 1200 || r.bytes >= 256<<20 {
		r.err = "capture budget reached"
		r.cancel()
		return
	}
	if ctx.Err() != nil {
		return
	}
	value, err := s.Engine.Run(ctx, r.sid, "human", "screenshot", map[string]any{})
	if err != nil {
		if ctx.Err() == nil {
			r.err = err.Error()
			r.cancel()
		}
		return
	}
	im, ok := value.(map[string]any)
	if !ok {
		r.err = "invalid screenshot result"
		r.cancel()
		return
	}
	encoded, _ := im["data"].(string)
	bytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(bytes) == 0 {
		r.err = "invalid screenshot data"
		r.cancel()
		return
	}
	if r.bytes+int64(len(bytes)) > 256<<20 {
		r.err = "capture byte budget"
		r.cancel()
		return
	}
	path := filepath.Join(r.dir, fmt.Sprintf("frame-%06d.png", r.frames))
	if err = store.Atomic(path, bytes, 0600); err != nil {
		r.err = err.Error()
		r.cancel()
		return
	}
	r.frames++
	r.bytes += int64(len(bytes))
	r.timestamps = append(r.timestamps, time.Now().UTC())
}
func (s *Server) stepRecording(sid string) {
	s.mu.Lock()
	r := s.recordings[sid]
	s.mu.Unlock()
	if r == nil || r.mode != "steps" {
		return
	}
	select {
	case <-r.done:
		return
	default:
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		s.capture(ctx, r)
	}
}
