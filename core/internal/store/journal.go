package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Options struct {
	MaxJournalBytes  int64
	RetainedSegments int
	MaxMemoryEvents  int
	MaxDocumentBytes int
}

func defaults(o Options) Options {
	if o.MaxJournalBytes <= 0 {
		o.MaxJournalBytes = 16 << 20
	}
	if o.RetainedSegments <= 0 {
		o.RetainedSegments = 8
	}
	if o.MaxMemoryEvents <= 0 {
		o.MaxMemoryEvents = 4096
	}
	if o.MaxDocumentBytes <= 0 {
		o.MaxDocumentBytes = 64 << 20
	}
	return o
}

func (s *Store) journalFiles() ([]string, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "events-") && strings.HasSuffix(e.Name(), ".jsonl") && !e.IsDir() {
			if e.Type()&os.ModeSymlink != 0 {
				return nil, errors.New("journal segment must not be a symlink")
			}
			paths = append(paths, filepath.Join(s.Root, e.Name()))
		}
	}
	sort.Strings(paths)
	return paths, nil
}
func (s *Store) remember(ev Event) {
	s.events = append(s.events, ev)
	if len(s.events) > s.options.MaxMemoryEvents*2 {
		// Copy, rather than slicing, so old large payloads can be collected.
		s.events = append([]Event(nil), s.events[len(s.events)-s.options.MaxMemoryEvents:]...)
	}
}
func (s *Store) replay(f *os.File, repairTail bool) error {
	r := bufio.NewReaderSize(f, 64<<10)
	var offset int64
	for {
		line, err := readJournalLine(r)
		if err == io.EOF {
			if len(line) > 0 {
				if !repairTail {
					return fmt.Errorf("torn immutable journal segment at %d", offset)
				}
				if err = f.Truncate(offset); err != nil {
					return err
				}
				if err = f.Sync(); err != nil {
					return err
				}
			}
			return nil
		}
		if err != nil {
			return err
		}
		var ev Event
		if err = json.Unmarshal(line, &ev); err != nil {
			return fmt.Errorf("corrupt journal at byte %d: %w", offset, err)
		}
		if ev.Seq <= s.seq {
			return errors.New("journal sequence is not monotonic")
		}
		if s.floor == 0 {
			s.floor = ev.Seq
		}
		s.seq = ev.Seq
		s.remember(ev)
		offset += int64(len(line))
	}
}

// ReadBytes alone allocates without a limit on a malicious or corrupt line.
func readJournalLine(r *bufio.Reader) ([]byte, error) {
	var result []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(result)+len(part) > 8<<20 {
			return nil, errors.New("oversized journal entry")
		}
		result = append(result, part...)
		if err != bufio.ErrBufferFull {
			return result, err
		}
	}
}
func (s *Store) rotate() error {
	if err := s.log.Sync(); err != nil {
		return err
	}
	if err := s.log.Close(); err != nil {
		return err
	}
	name := filepath.Join(s.Root, fmt.Sprintf("events-%020d.jsonl", s.seq))
	if err := os.Rename(filepath.Join(s.Root, "events.jsonl"), name); err != nil {
		s.closed = true
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Root, "events.jsonl"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		s.closed = true
		return err
	}
	s.log, s.logBytes = f, 0
	if err := syncDir(s.Root); err != nil {
		return err
	}
	return nil
}
func (s *Store) trimSegments() error {
	files, err := s.journalFiles()
	if err != nil {
		return err
	}
	if len(files) <= s.options.RetainedSegments {
		return nil
	}
	for _, f := range files[:len(files)-s.options.RetainedSegments] {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	first := files[len(files)-s.options.RetainedSegments]
	f, err := os.Open(first)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := readJournalLine(bufio.NewReader(f))
	if err != nil {
		return err
	}
	var ev Event
	if err := json.Unmarshal(line, &ev); err != nil {
		return err
	}
	s.floor = ev.Seq
	cut := 0
	for cut < len(s.events) && s.events[cut].Seq < s.floor {
		cut++
	}
	if cut > 0 {
		s.events = append([]Event(nil), s.events[cut:]...)
	}
	return syncDir(s.Root)
}
func syncDir(path string) error {
	// Directory fsync is unavailable on Windows. Atomic rename is still used;
	// platform power-loss guarantees are documented rather than fabricated.
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_ = f.Sync()
	return nil
}

type JournalRange struct {
	First            uint64 `json:"first"`
	Last             uint64 `json:"last"`
	MemoryEvents     int    `json:"memoryEvents"`
	MaxSegmentBytes  int64  `json:"maxSegmentBytes"`
	RetainedSegments int    `json:"retainedSegments"`
}

func (s *Store) Range() JournalRange {
	s.mu.Lock()
	defer s.mu.Unlock()
	return JournalRange{s.floor, s.seq, len(s.events), s.options.MaxJournalBytes, s.options.RetainedSegments}
}

// eventsLocked falls back to retained disk segments when a consumer reconnects
// outside the bounded in-memory window. Expired cursors are reported by Range.
func (s *Store) eventsLocked(after uint64, session string, limit int) []Event {
	out := []Event{}
	add := func(ev Event) bool {
		if ev.Seq > after && (session == "" || session == ev.Session) {
			out = append(out, ev)
		}
		return len(out) >= limit
	}
	if len(s.events) == 0 || after+1 >= s.events[0].Seq {
		for _, ev := range s.events {
			if add(ev) {
				break
			}
		}
		return out
	}
	paths, err := s.journalFiles()
	if err != nil {
		return out
	}
	paths = append(paths, filepath.Join(s.Root, "events.jsonl"))
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		r := bufio.NewReader(f)
		for {
			b, err := readJournalLine(r)
			if err != nil {
				break
			}
			var ev Event
			if json.Unmarshal(b, &ev) != nil {
				break
			}
			if add(ev) {
				f.Close()
				return out
			}
		}
		f.Close()
	}
	return out
}

// PurgeSession rewrites every retained segment, preserving other sessions and
// sequence numbers. A payload-free checkpoint preserves the cursor on restart.
func (s *Store) PurgeSession(session string) error {
	if !ValidID(session) {
		return errors.New("invalid session id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	paths, err := s.journalFiles()
	if err != nil {
		return err
	}
	current := filepath.Join(s.Root, "events.jsonl")
	paths = append(paths, current)
	if err = s.log.Sync(); err != nil {
		return err
	}
	for _, path := range paths {
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		reader := bufio.NewReader(f)
		kept := []byte{}
		for {
			line, e := readJournalLine(reader)
			if e == io.EOF {
				break
			}
			if e != nil {
				f.Close()
				return e
			}
			var event Event
			if e = json.Unmarshal(line, &event); e != nil {
				f.Close()
				return e
			}
			if event.Session != session {
				kept = append(kept, line...)
			}
		}
		f.Close()
		if path == current {
			checkpoint, e := json.Marshal(Event{Seq: s.seq + 1, Type: "journal.checkpoint"})
			if e != nil {
				return e
			}
			kept = append(kept, append(checkpoint, '\n')...)
			if e = s.log.Close(); e != nil {
				return e
			}
		}
		if e = Atomic(path, kept, 0600); e != nil {
			if path == current {
				s.log, _ = os.OpenFile(current, os.O_RDWR|os.O_APPEND, 0600)
				if s.log == nil {
					s.closed = true
				}
			}
			return e
		}
	}
	s.log, err = os.OpenFile(current, os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		s.closed = true
		return err
	}
	s.seq++
	s.events = nil
	s.floor = 0
	// Replay only into a temporary cursor, retaining the original sequence floor.
	sequence := s.seq
	s.seq = 0
	for _, path := range paths {
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		e = s.replay(f, false)
		f.Close()
		if e != nil {
			return e
		}
	}
	if s.seq != sequence {
		return errors.New("journal checkpoint mismatch")
	}
	info, err := s.log.Stat()
	if err != nil {
		return err
	}
	s.logBytes = info.Size()
	close(s.wake)
	s.wake = make(chan struct{})
	return nil
}
