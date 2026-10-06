// Package store provides atomic JSON documents and a durable, replayable event journal.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var nameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,120}$`)

func ID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func ValidID(s string) bool { return nameRE.MatchString(s) && s != "." && s != ".." }

type Event struct {
	Seq     uint64    `json:"seq"`
	At      time.Time `json:"at"`
	Session string    `json:"session,omitempty"`
	Type    string    `json:"type"`
	Data    any       `json:"data,omitempty"`
}
type Store struct {
	Root     string
	mu       sync.Mutex
	events   []Event
	seq      uint64
	log      *os.File
	closed   bool
	lock     *os.File
	options  Options
	logBytes int64
	floor    uint64
	wake     chan struct{}
}

func Open(root string) (*Store, error) { return OpenWithOptions(root, Options{}) }
func OpenWithOptions(root string, opts Options) (*Store, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	for _, name := range []string{root, filepath.Join(root, ".owner.lock"), filepath.Join(root, "events.jsonl")} {
		info, e := os.Lstat(name)
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("private state must not use symbolic links")
		}
	}
	lock, err := acquireLock(filepath.Join(root, ".owner.lock"))
	if err != nil {
		return nil, err
	}
	s := &Store{Root: root, lock: lock, options: defaults(opts), wake: make(chan struct{})}
	fail := func(e error) (*Store, error) {
		if s.log != nil {
			s.log.Close()
		}
		lock.Close()
		return nil, e
	}
	files, err := s.journalFiles()
	if err != nil {
		return fail(err)
	}
	for _, path := range files {
		f, e := os.Open(path)
		if e != nil {
			return fail(e)
		}
		e = s.replay(f, false)
		f.Close()
		if e != nil {
			return fail(e)
		}
	}
	f, err := os.OpenFile(filepath.Join(root, "events.jsonl"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fail(err)
	}
	s.log = f
	if err = s.replay(f, true); err != nil {
		return fail(err)
	}
	s.logBytes, err = f.Seek(0, 2)
	if err != nil {
		return fail(err)
	}
	return s, nil
}
func (s *Store) Read(name string, out any) error {
	if !ValidID(name) {
		return errors.New("invalid document name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.Root, name+".json")
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Size() > int64(s.options.MaxDocumentBytes) {
		return errors.New("unsafe or oversized document")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func (s *Store) Write(name string, v any) error {
	if !ValidID(name) {
		return errors.New("invalid document name")
	}
	b, e := json.MarshalIndent(v, "", "  ")
	if len(b) > s.options.MaxDocumentBytes {
		return errors.New("document quota exceeded")
	}
	if e != nil {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	return Atomic(filepath.Join(s.Root, name+".json"), append(b, '\n'), 0600)
}
func Atomic(path string, b []byte, perm os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".atomic-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(perm); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
func (s *Store) Append(session, kind string, data any) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Event{}, errors.New("store closed")
	}
	ev := Event{Seq: s.seq + 1, At: time.Now().UTC(), Session: session, Type: kind, Data: data}
	b, e := json.Marshal(ev)
	if e != nil {
		return Event{}, e
	}
	if len(b) > 4<<20 {
		return Event{}, errors.New("event too large")
	}
	if s.logBytes > 0 && s.logBytes+int64(len(b))+1 > s.options.MaxJournalBytes {
		if e = s.rotate(); e != nil {
			return Event{}, e
		}
	}
	if _, e = s.log.Write(append(b, '\n')); e != nil {
		return Event{}, e
	}
	if e = s.log.Sync(); e != nil {
		return Event{}, e
	}
	s.seq = ev.Seq
	s.logBytes += int64(len(b)) + 1
	if s.floor == 0 {
		s.floor = ev.Seq
	}
	s.remember(ev)
	close(s.wake)
	s.wake = make(chan struct{})
	if e = s.trimSegments(); e != nil {
		return ev, fmt.Errorf("event committed but journal retention failed: %w", e)
	}
	return ev, nil
}
func (s *Store) Events(after uint64, session string, limit int) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 || limit > 1000 {
		limit = 1000
	}
	return s.eventsLocked(after, session, limit)
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == nil {
		return nil
	}
	s.closed = true
	close(s.wake)
	s.wake = make(chan struct{})
	err := s.log.Close()
	lockErr := s.lock.Close()
	s.lock = nil
	if err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	return lockErr
}

// Remove deletes a validated document; missing documents are already removed.
func (s *Store) Remove(name string) error {
	if !ValidID(name) {
		return errors.New("invalid document name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	err := os.Remove(filepath.Join(s.Root, name+".json"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
