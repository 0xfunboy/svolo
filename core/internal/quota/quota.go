package quota

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"svolo.local/core/internal/store"
	"sync"
)

type Limits struct {
	Bytes         int64 `json:"bytes"`
	SessionBytes  int64 `json:"sessionBytes"`
	Files         int   `json:"files"`
	SessionFiles  int   `json:"sessionFiles"`
	DownloadBytes int64 `json:"downloadBytes"`
}
type Usage struct {
	Bytes int64 `json:"bytes"`
	Files int   `json:"files"`
}
type Snapshot struct {
	Limits   Limits           `json:"limits"`
	Total    Usage            `json:"total"`
	Sessions map[string]Usage `json:"sessions"`
	Over     bool             `json:"over"`
	Scope    string           `json:"scope"`
}

var gate sync.Mutex

func Defaults() Limits {
	return Limits{Bytes: 2 << 30, SessionBytes: 512 << 20, Files: 8192, SessionFiles: 4096, DownloadBytes: 64 << 20}
}
func (l Limits) Validate() error {
	if l.Bytes < 1<<20 || l.Bytes > 1<<40 || l.SessionBytes < 1<<20 || l.SessionBytes > l.Bytes || l.Files < 4 || l.Files > 200000 || l.SessionFiles < 2 || l.SessionFiles > l.Files || l.DownloadBytes < 1 || l.DownloadBytes > l.SessionBytes {
		return errors.New("invalid storage quota")
	}
	return nil
}
func load(root string) (Limits, error) {
	l := Defaults()
	b, e := os.ReadFile(filepath.Join(root, "storage-limits.json"))
	if e == nil {
		e = json.Unmarshal(b, &l)
	} else if os.IsNotExist(e) {
		e = nil
	}
	if e == nil {
		e = l.Validate()
	}
	return l, e
}
func Set(root string, l Limits) error {
	if e := l.Validate(); e != nil {
		return e
	}
	gate.Lock()
	defer gate.Unlock()
	b, _ := json.MarshalIndent(l, "", "  ")
	return store.Atomic(filepath.Join(root, "storage-limits.json"), b, 0600)
}
func Measure(root string) (Snapshot, error) { gate.Lock(); defer gate.Unlock(); return measure(root) }
func measure(root string) (Snapshot, error) {
	l, e := load(root)
	s := Snapshot{Limits: l, Sessions: map[string]Usage{}, Scope: "artifacts + recording cache + browser downloads + transfer staging; excludes profiles, workspace files, system caches and logs"}
	if e != nil {
		return s, e
	}
	visited := 0
	for _, kind := range []string{"artifacts", "recordings", "downloads", "transfers"} {
		base := filepath.Join(root, kind)
		e = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if os.IsNotExist(err) && path == base {
				return nil
			}
			if err != nil {
				return err
			}
			visited++
			if visited > 250000 {
				return errors.New("quota scan entry budget exceeded")
			}
			if d.Type()&os.ModeSymlink != 0 {
				return errors.New("symlinks refused in private artifact caches")
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return errors.New("nonregular artifact-cache entry")
			}
			size := info.Size()
			s.Total.Bytes += size
			s.Total.Files++
			rel, err := filepath.Rel(base, path)
			if err != nil {
				return err
			}
			parts := strings.Split(rel, string(os.PathSeparator))
			sid := ""
			if kind != "transfers" && len(parts) > 1 {
				sid = parts[0]
			} else if kind == "transfers" {
				id := strings.TrimSuffix(parts[0], ".part")
				if store.ValidID(id) {
					b, er := os.ReadFile(filepath.Join(root, "transfer-"+id+".json"))
					if er == nil {
						var state struct {
							Session string `json:"session"`
						}
						if json.Unmarshal(b, &state) == nil {
							sid = state.Session
						}
					}
				}
			}
			if sid != "" {
				u := s.Sessions[sid]
				u.Bytes += size
				u.Files++
				s.Sessions[sid] = u
			}
			return nil
		})
		if e != nil {
			return s, e
		}
	}
	s.Over = s.Total.Bytes > l.Bytes || s.Total.Files > l.Files
	for _, u := range s.Sessions {
		if u.Bytes > l.SessionBytes || u.Files > l.SessionFiles {
			s.Over = true
		}
	}
	return s, nil
}
func Check(root, sid string, addBytes int64, addFiles int) error {
	gate.Lock()
	defer gate.Unlock()
	s, e := measure(root)
	if e != nil {
		return e
	}
	return check(s, sid, addBytes, addFiles)
}
func check(s Snapshot, sid string, n int64, files int) error {
	u := s.Sessions[sid]
	if n < 0 || files < 0 {
		return errors.New("negative storage reservation")
	}
	if n > s.Limits.Bytes-s.Total.Bytes || files > s.Limits.Files-s.Total.Files || n > s.Limits.SessionBytes-u.Bytes || files > s.Limits.SessionFiles-u.Files {
		return fmt.Errorf("storage quota exceeded for %s; inspect /v1/storage, remove explicitly selected caches or raise the owner-set limit", sid)
	}
	return nil
}
func Within(root, sid string, n int64, files int, fn func() error) error {
	gate.Lock()
	defer gate.Unlock()
	s, e := measure(root)
	if e != nil {
		return e
	}
	if e = check(s, sid, n, files); e != nil {
		return e
	}
	return fn()
}
