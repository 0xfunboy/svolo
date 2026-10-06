package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"svolo.local/core/internal/store"
)

func (m *Managed) ImportProfile(ctx context.Context, sid, source string, force bool) error {
	if !store.ValidID(sid) || !filepath.IsAbs(source) {
		return errors.New("session id and absolute source profile path required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.processes[sid] != nil {
		return errors.New("close the managed browser before importing its profile")
	}
	source, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	for _, name := range []string{"SingletonLock", "SingletonSocket", "lockfile"} {
		if _, err := os.Lstat(filepath.Join(source, name)); err == nil {
			return fmt.Errorf("source profile is open or locked (%s); close it before importing", name)
		}
	}
	dest := filepath.Join(m.opts.Root, "profiles", sid)
	rel, _ := filepath.Rel(source, dest)
	if rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..") {
		return errors.New("source cannot contain the destination")
	}
	if _, err = os.Stat(dest); err == nil && !force {
		return errors.New("destination exists; explicit force required (creates a backup)")
	}
	temp := dest + ".import-" + store.ID()
	if err = os.MkdirAll(temp, 0700); err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	var total int64
	count := 0
	err = filepath.WalkDir(source, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("profile contains symlink: %s", rel)
		}
		if d.IsDir() {
			if d.Name() == "Cache" || d.Name() == "Code Cache" || d.Name() == "GPUCache" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(temp, rel), 0700)
		}
		if d.Name() == "DevToolsActivePort" || strings.HasPrefix(d.Name(), "Singleton") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular profile file: %s", rel)
		}
		count++
		total += info.Size()
		if count > 50000 || total > 2<<30 {
			return errors.New("profile import size/file budget exceeded")
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(filepath.Join(temp, rel), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, io.LimitReader(in, info.Size()+1))
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		return err
	}
	backup := ""
	if _, err = os.Stat(dest); err == nil {
		backup = dest + ".backup-" + store.ID()
		if err = os.Rename(dest, backup); err != nil {
			return err
		}
	}
	if err = os.Rename(temp, dest); err != nil {
		if backup != "" {
			_ = os.Rename(backup, dest)
		}
		return err
	}
	return nil
}
func (e *Engine) ConfigureDownloads(ctx context.Context, sid string) error {
	s, err := e.get(sid)
	if err != nil {
		return err
	}
	s.op.Lock()
	defer s.op.Unlock()
	tid, err := e.target(ctx, sid, s, nil)
	if err != nil {
		return err
	}
	path := filepath.Join(e.Data, "downloads", sid)
	if err = os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return e.Transport.Call(ctx, sid, tid, "Browser.setDownloadBehavior", map[string]any{"behavior": "allow", "downloadPath": path, "eventsEnabled": true}, nil)
}
