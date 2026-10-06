// Package transfer implements resumable, content-verified transfers. Uploads are
// staged privately and become workspace files only after checksum and conflict
// checks. A disconnect does not restart the transfer or overwrite an existing file.
package transfer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"svolo.local/core/internal/store"
	"svolo.local/core/internal/workspace"
)

const ChunkSize = 1 << 20
const MaxSize int64 = 256 << 20

var shaRE = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Spec struct {
	Session           string `json:"session"`
	Path              string `json:"path"`
	Size              int64  `json:"size"`
	SHA256            string `json:"sha256"`
	ExpectedOldSHA256 string `json:"expectedOldSha256,omitempty"`
}
type State struct {
	Spec
	ID        string    `json:"id"`
	Root      string    `json:"root"`
	Offset    int64     `json:"offset"`
	Direction string    `json:"direction"`
	Status    string    `json:"status"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
}
type Manager struct {
	mu     sync.Mutex
	store  *store.Store
	root   string
	states map[string]State
}

func New(st *store.Store) (*Manager, error) {
	m := &Manager{store: st, root: filepath.Join(st.Root, "transfers"), states: map[string]State{}}
	if err := os.MkdirAll(m.root, 0700); err != nil {
		return nil, err
	}
	files, err := os.ReadDir(st.Root)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		name := file.Name()
		if len(name) < 14 || name[:9] != "transfer-" || filepath.Ext(name) != ".json" {
			continue
		}
		var state State
		if err := st.Read(name[:len(name)-5], &state); err != nil {
			return nil, err
		}
		if !store.ValidID(state.ID) || !store.ValidID(state.Session) {
			return nil, errors.New("invalid transfer state")
		}
		// Bytes not checkpointed before a crash are rolled back, never assumed committed.
		if state.Status == "receiving" {
			f, e := os.OpenFile(m.part(state.ID), os.O_RDWR, 0600)
			if e != nil {
				state.Status = "failed"
			} else {
				info, e := f.Stat()
				if e != nil || info.Size() < state.Offset {
					state.Status = "failed"
				} else {
					e = f.Truncate(state.Offset)
					if e != nil {
						f.Close()
						return nil, e
					}
				}
				f.Close()
			}
		}
		m.states[state.ID] = state
	}
	return m, nil
}
func (m *Manager) part(id string) string { return filepath.Join(m.root, id+".part") }
func (m *Manager) save(state State) error {
	state.Updated = time.Now().UTC()
	if err := m.store.Write("transfer-"+state.ID, state); err != nil {
		return err
	}
	m.states[state.ID] = state
	return nil
}
func (m *Manager) quota(add int64) error {
	var total int64
	active := 0
	for _, s := range m.states {
		if s.Status == "receiving" || s.Status == "ready" {
			active++
			total += s.Size
		}
	}
	if active >= 16 || total+add > 1<<30 {
		return errors.New("transfer storage quota exceeded; abort or remove completed downloads")
	}
	return nil
}
func (m *Manager) Start(root string, spec Spec) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !store.ValidID(spec.Session) || spec.Size < 0 || spec.Size > MaxSize || !shaRE.MatchString(spec.SHA256) {
		return State{}, errors.New("invalid transfer specification")
	}
	if spec.ExpectedOldSHA256 != "" && !shaRE.MatchString(spec.ExpectedOldSHA256) {
		return State{}, errors.New("invalid expected old checksum")
	}
	w, err := workspace.Open(root)
	if err != nil {
		return State{}, err
	}
	path, err := w.Path(spec.Path, true)
	if err != nil || path == w.Root {
		return State{}, errors.New("invalid destination path")
	}
	if err = m.quota(spec.Size); err != nil {
		return State{}, err
	}
	if info, e := os.Lstat(path); e == nil {
		if !info.Mode().IsRegular() || spec.ExpectedOldSHA256 == "" {
			return State{}, errors.New("destination exists; explicit previous checksum required")
		}
	} else if !os.IsNotExist(e) {
		return State{}, e
	}
	state := State{Spec: spec, ID: store.ID(), Root: w.Root, Direction: "upload", Status: "receiving", Created: time.Now().UTC()}
	f, err := os.OpenFile(m.part(state.ID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return State{}, err
	}
	f.Close()
	if err = m.save(state); err != nil {
		os.Remove(m.part(state.ID))
		return State{}, err
	}
	return m.states[state.ID], nil
}
func (m *Manager) Get(sid, id string) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.get(sid, id)
}
func (m *Manager) get(sid, id string) (State, error) {
	s, ok := m.states[id]
	if !ok || s.Session != sid {
		return State{}, errors.New("unknown transfer in this session")
	}
	return s, nil
}
func (m *Manager) List(sid string) []State {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []State{}
	for _, s := range m.states {
		if s.Session == sid {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}
func (m *Manager) Put(sid, id string, offset int64, data []byte) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.get(sid, id)
	if err != nil {
		return s, err
	}
	if s.Status != "receiving" || s.Direction != "upload" {
		return s, errors.New("transfer is not accepting chunks")
	}
	if len(data) == 0 || len(data) > ChunkSize || offset < 0 || offset+int64(len(data)) > s.Size {
		return s, errors.New("invalid chunk bounds")
	}
	f, err := os.OpenFile(m.part(id), os.O_RDWR, 0600)
	if err != nil {
		return s, err
	}
	defer f.Close()
	if offset < s.Offset {
		// Idempotent acknowledgement ONLY for identical bytes, never for another write.
		if offset+int64(len(data)) > s.Offset {
			return s, errors.New("overlapping uncommitted chunk")
		}
		old := make([]byte, len(data))
		if _, err = f.ReadAt(old, offset); err != nil {
			return s, err
		}
		if !bytes.Equal(old, data) {
			return s, errors.New("replayed chunk differs from checkpoint")
		}
		return s, nil
	}
	if offset != s.Offset {
		return s, fmt.Errorf("offset conflict: expected %d", s.Offset)
	}
	if _, err = f.WriteAt(data, offset); err != nil {
		return s, err
	}
	if err = f.Sync(); err != nil {
		return s, err
	}
	old := s.Offset
	s.Offset += int64(len(data))
	if err = m.save(s); err != nil {
		_ = f.Truncate(old)
		return s, err
	}
	return m.states[id], nil
}
func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return "", 0, errors.New("not a regular file")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, MaxSize+1))
	if err != nil {
		return "", n, err
	}
	if n > MaxSize {
		return "", n, errors.New("file exceeds transfer budget")
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
func (m *Manager) Commit(sid, id, workspaceRoot string) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.get(sid, id)
	if err != nil {
		return s, err
	}
	if s.Status == "completed" {
		return s, nil
	}
	if s.Status != "receiving" || s.Direction != "upload" || s.Offset != s.Size {
		return s, errors.New("upload is incomplete")
	}
	w, err := workspace.Open(workspaceRoot)
	if err != nil {
		return s, err
	}
	if w.Root != s.Root {
		return s, errors.New("workspace changed during transfer")
	}
	path, err := w.Path(s.Path, true)
	if err != nil {
		return s, err
	}
	hash, n, err := hashFile(m.part(id))
	if err != nil {
		return s, err
	}
	if hash != s.SHA256 || n != s.Size {
		return s, errors.New("upload checksum mismatch; destination unchanged")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return s, err
	}
	if _, err = w.Path(s.Path, true); err != nil {
		return s, err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".svolo-transfer-*")
	if err != nil {
		return s, err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	source, err := os.Open(m.part(id))
	if err != nil {
		temp.Close()
		return s, err
	}
	_, err = io.Copy(temp, source)
	source.Close()
	if err == nil {
		err = temp.Chmod(0600)
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return s, err
	}
	if s.ExpectedOldSHA256 == "" {
		// A hard link gives atomic no-replace semantics on supported filesystems.
		// Refuse unsupported filesystems instead of falling back to unsafe clobbering.
		err = os.Link(tempName, path)
	} else {
		old, _, e := hashFile(path)
		if e != nil || old != s.ExpectedOldSHA256 {
			return s, errors.New("destination changed since approval")
		}
		err = os.Rename(tempName, path)
	}
	if err != nil {
		return s, err
	}
	s.Status = "completed"
	if err = m.save(s); err != nil {
		return s, errors.New("destination committed but checkpoint failed; verify file hash before retrying")
	}
	_ = os.Remove(m.part(id))
	return m.states[id], nil
}
func (m *Manager) Download(root, sid, path string) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !store.ValidID(sid) {
		return State{}, errors.New("invalid session")
	}
	if err := m.quota(MaxSize); err != nil {
		return State{}, err
	}
	w, err := workspace.Open(root)
	if err != nil {
		return State{}, err
	}
	absolute, err := w.Path(path, false)
	if err != nil {
		return State{}, err
	}
	hash, n, err := hashFile(absolute)
	if err != nil {
		return State{}, err
	}
	state := State{Spec: Spec{Session: sid, Path: path, Size: n, SHA256: hash}, ID: store.ID(), Root: w.Root, Offset: n, Direction: "download", Status: "ready", Created: time.Now().UTC()}
	src, err := os.Open(absolute)
	if err != nil {
		return state, err
	}
	defer src.Close()
	dst, err := os.OpenFile(m.part(state.ID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return state, err
	}
	_, err = io.Copy(dst, io.LimitReader(src, MaxSize+1))
	if err == nil {
		err = dst.Sync()
	}
	dst.Close()
	if err != nil {
		os.Remove(m.part(state.ID))
		return state, err
	}
	copyHash, copyN, err := hashFile(m.part(state.ID))
	if err != nil || copyHash != hash || copyN != n {
		os.Remove(m.part(state.ID))
		return state, errors.New("source changed during immutable download snapshot")
	}
	if err = m.save(state); err != nil {
		os.Remove(m.part(state.ID))
		return state, err
	}
	return m.states[state.ID], nil
}
func (m *Manager) Read(sid, id string, offset int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.get(sid, id)
	if err != nil {
		return nil, err
	}
	if s.Direction != "download" || s.Status != "ready" || offset < 0 || offset > s.Size {
		return nil, errors.New("invalid download chunk")
	}
	f, err := os.Open(m.part(id))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err = f.Seek(offset, 0); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(f, ChunkSize))
}
func (m *Manager) Abort(sid, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.get(sid, id)
	if err != nil {
		return err
	}
	if err = os.Remove(m.part(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	s.Status = "aborted"
	return m.save(s)
}
