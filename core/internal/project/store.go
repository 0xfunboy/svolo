package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"svolo.local/core/internal/store"
	"sync"
	"time"
)

type Snapshot struct {
	Value    Object            `json:"value"`
	Revision uint64            `json:"revision"`
	Receipts map[string]string `json:"receipts,omitempty"`
}
type Mutation struct {
	Op               Object `json:"op"`
	ExpectedRevision uint64 `json:"expectedRevision"`
	OperationID      string `json:"operationId"`
}
type Repository struct {
	mu     sync.Mutex
	store  *store.Store
	values map[string]Snapshot
}

func New(s *store.Store) (*Repository, error) {
	r := &Repository{store: s, values: map[string]Snapshot{}}
	for _, domain := range []string{"board", "laments"} {
		v := Snapshot{Value: Empty(domain), Receipts: map[string]string{}}
		if e := s.Read("project-"+domain, &v); e != nil && !os.IsNotExist(e) {
			return nil, e
		}
		clean, e := Validate(domain, v.Value)
		if e != nil {
			return nil, e
		}
		v.Value = clean
		if v.Receipts == nil {
			v.Receipts = map[string]string{}
		}
		r.values[domain] = v
	}
	return r, nil
}
func public(s Snapshot) Snapshot { return Snapshot{Value: copyValue(s.Value), Revision: s.Revision} }
func (r *Repository) Get(domain string) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.values[domain]
	if !ok {
		return Snapshot{}, errors.New("unknown domain")
	}
	return public(s), nil
}
func (r *Repository) Mutate(domain string, m Mutation) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.values[domain]
	if !ok {
		return Snapshot{}, errors.New("unknown domain")
	}
	if !store.ValidID(m.OperationID) {
		return Snapshot{}, errors.New("operationId is required for idempotent mutation")
	}
	raw, e := json.Marshal(m.Op)
	if e != nil {
		return Snapshot{}, e
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	if prior, ok := s.Receipts[m.OperationID]; ok {
		if prior != hash {
			return Snapshot{}, errors.New("operationId reused for different content")
		}
		return public(s), nil
	}
	if m.ExpectedRevision != s.Revision {
		return Snapshot{}, errors.New("revision conflict; refresh before issuing another operation")
	}
	var value Object
	if m.Op["type"] == "import" {
		if s.Revision != 0 {
			return Snapshot{}, errors.New("migration cannot overwrite existing data")
		}
		value, e = Validate(domain, obj(m.Op["value"]))
	} else {
		value, e = Apply(domain, s.Value, m.Op, time.Now().UnixMilli())
	}
	if e != nil {
		return Snapshot{}, e
	}
	next := Snapshot{Value: value, Revision: s.Revision + 1, Receipts: map[string]string{}}
	// Retain a bounded idempotency window without storing duplicate snapshots.
	if len(s.Receipts) < 256 {
		for k, v := range s.Receipts {
			next.Receipts[k] = v
		}
	}
	next.Receipts[m.OperationID] = hash
	if e = r.store.Write("project-"+domain, next); e != nil {
		return Snapshot{}, e
	}
	r.values[domain] = next
	_, e = r.store.Append("", "project."+domain, map[string]any{"revision": next.Revision, "operationId": m.OperationID, "operation": m.Op["type"]})
	return public(next), e
}
