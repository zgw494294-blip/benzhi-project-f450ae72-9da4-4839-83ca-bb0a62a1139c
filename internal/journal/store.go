package journal

import (
	"caption-delivery-qc/internal/domain"
	"encoding/json"
	"os"
	"sync"
	"time"
)

type Store struct {
	mu          sync.RWMutex
	jobs        map[string]*domain.ReviewJob
	credentials map[string]domain.Credential
	idempotency map[string]IdempotencyRecord
	events      []domain.Event
	path        string
	seq         int64
	prev        string
}
type IdempotencyRecord struct {
	Actor      string                 `json:"actor"`
	Digest     string                 `json:"digest"`
	JobID      string                 `json:"jobID"`
	Version    int64                  `json:"version,omitempty"`
	CueResults []domain.CueEditResult `json:"cueResults,omitempty"`
	JobResult  *domain.ReviewJob      `json:"jobResult,omitempty"`
}
type persistedState struct {
	Jobs           map[string]*domain.ReviewJob `json:"jobs"`
	Credentials    map[string]domain.Credential `json:"credentials"`
	Idempotency    map[string]IdempotencyRecord `json:"idempotency"`
	Events         []domain.Event               `json:"events"`
	Sequence       int64                        `json:"sequence"`
	PreviousDigest string                       `json:"previousDigest"`
}

func New(path string) (*Store, error) {
	s := &Store{jobs: map[string]*domain.ReviewJob{}, credentials: map[string]domain.Credential{}, idempotency: map[string]IdempotencyRecord{}, path: path}
	if path != "" {
		if b, e := os.ReadFile(path); e == nil && len(b) > 0 {
			var p persistedState
			if e = json.Unmarshal(b, &p); e != nil {
				return nil, e
			}
			if p.Jobs != nil {
				s.jobs = p.Jobs
			}
			if p.Credentials != nil {
				s.credentials = p.Credentials
			}
			if p.Idempotency != nil {
				s.idempotency = p.Idempotency
			}
			// Recover the full, untampered journal or fail closed. A broken
			// event chain — whether mid-journal or at the tail — means the
			// persisted job/credential/idempotency projections may reflect
			// events that no longer verify. Truncating events to a valid
			// prefix while keeping those projections would desync the timeline
			// (prefix only) from the detail view (full projection), so reject
			// the state entirely instead of exposing inconsistent data.
			if !Replay(p.Events) {
				return nil, domain.ErrIntegrity
			}
			var lastSeq int64
			var lastPrev string
			for _, e := range p.Events {
				lastSeq = e.Sequence
				lastPrev = e.Digest
			}
			if p.Sequence != lastSeq || p.PreviousDigest != lastPrev {
				return nil, domain.ErrIntegrity
			}
			s.events = p.Events
			s.seq = p.Sequence
			s.prev = p.PreviousDigest
		}
	}
	return s, nil
}
func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	b, e := json.Marshal(persistedState{Jobs: s.jobs, Credentials: s.credentials, Idempotency: s.idempotency, Events: s.events, Sequence: s.seq, PreviousDigest: s.prev})
	if e != nil {
		return e
	}
	tmp := s.path + ".tmp"
	if e = os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, s.path)
}
func (s *Store) Create(j *domain.ReviewJob, key string, record ...IdempotencyRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key != "" {
		if _, ok := s.idempotency[key]; ok {
			return nil
		}
	}
	oldEvents, oldSeq, oldPrev := len(s.events), s.seq, s.prev
	s.jobs[j.ID] = clone(j)
	s.appendLocked("job.created", j.ID, j.Version, "system", j)
	if key != "" {
		if len(record) > 0 {
			s.idempotency[key] = record[0]
		} else {
			s.idempotency[key] = IdempotencyRecord{JobID: j.ID}
		}
	}
	if e := s.persistLocked(); e != nil {
		delete(s.jobs, j.ID)
		if key != "" {
			delete(s.idempotency, key)
		}
		s.events = s.events[:oldEvents]
		s.seq = oldSeq
		s.prev = oldPrev
		return e
	}
	return nil
}
func (s *Store) Idempotency(key string) (IdempotencyRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.idempotency[key]
	return v, ok
}
func (s *Store) Get(id string) (*domain.ReviewJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return clone(j), nil
}
func (s *Store) Save(j *domain.ReviewJob, typ, actor string, data any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.jobs[j.ID]
	oldEvents, oldSeq, oldPrev := len(s.events), s.seq, s.prev
	s.jobs[j.ID] = clone(j)
	s.appendLocked(typ, j.ID, j.Version, actor, data)
	if e := s.persistLocked(); e != nil {
		if had {
			s.jobs[j.ID] = old
		} else {
			delete(s.jobs, j.ID)
		}
		s.events = s.events[:oldEvents]
		s.seq = oldSeq
		s.prev = oldPrev
		return e
	}
	return nil
}
func (s *Store) SaveIdempotent(j *domain.ReviewJob, typ, actor string, data any, key string, record IdempotencyRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldJob, hadJob := s.jobs[j.ID]
	oldRec, hadRec := s.idempotency[key]
	oldEvents, oldSeq, oldPrev := len(s.events), s.seq, s.prev
	s.jobs[j.ID] = clone(j)
	s.idempotency[key] = record
	s.appendLocked(typ, j.ID, j.Version, actor, data)
	if e := s.persistLocked(); e != nil {
		if hadJob {
			s.jobs[j.ID] = oldJob
		} else {
			delete(s.jobs, j.ID)
		}
		if hadRec {
			s.idempotency[key] = oldRec
		} else {
			delete(s.idempotency, key)
		}
		s.events = s.events[:oldEvents]
		s.seq = oldSeq
		s.prev = oldPrev
		return e
	}
	return nil
}
func (s *Store) PutCredential(c domain.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credentials[c.JobID] = c
	return s.persistLocked()
}
func (s *Store) CommitCredential(j *domain.ReviewJob, c domain.Credential, key string, record IdempotencyRecord, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldJob := s.jobs[j.ID]
	oldCred, hadCred := s.credentials[j.ID]
	oldRec, hadRec := s.idempotency[key]
	oldEvents, oldSeq, oldPrev := len(s.events), s.seq, s.prev
	s.jobs[j.ID] = clone(j)
	s.credentials[j.ID] = c
	if key != "" {
		s.idempotency[key] = record
	}
	s.appendLocked("credential.issued", j.ID, j.Version, actor, c)
	if e := s.persistLocked(); e != nil {
		s.jobs[j.ID] = oldJob
		if hadCred {
			s.credentials[j.ID] = oldCred
		} else {
			delete(s.credentials, j.ID)
		}
		if key != "" {
			if hadRec {
				s.idempotency[key] = oldRec
			} else {
				delete(s.idempotency, key)
			}
		}
		s.events = s.events[:oldEvents]
		s.seq = oldSeq
		s.prev = oldPrev
		return e
	}
	return nil
}
func (s *Store) Credential(id string) (domain.Credential, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.credentials[id]
	return c, ok
}
func (s *Store) Events(id string) []domain.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []domain.Event{}
	for _, e := range s.events {
		if e.JobID == id {
			out = append(out, e)
		}
	}
	return out
}

type EventQuery struct {
	JobID, Type, Actor string
	Cursor             int64
	Limit              int
}
type EventPage struct {
	Events      []domain.Event `json:"events"`
	NextCursor  int64          `json:"nextCursor,omitempty"`
	FirstDigest string         `json:"firstDigest,omitempty"`
	LastDigest  string         `json:"lastDigest,omitempty"`
	ChainDigest string         `json:"chainDigest"`
}

func (s *Store) PageEvents(q EventQuery) (EventPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prev := ""
	var seq int64
	for _, e := range s.events {
		if (seq != 0 && e.Sequence != seq+1) || e.PrevDigest != prev || e.Digest != domain.EventRecordDigest(e) {
			return EventPage{}, domain.ErrIntegrity
		}
		seq = e.Sequence
		prev = e.Digest
	}
	jobEvents := []domain.Event{}
	filtered := []domain.Event{}
	for _, e := range s.events {
		if q.JobID != "" && e.JobID == q.JobID {
			jobEvents = append(jobEvents, e)
		}
		if e.Sequence <= q.Cursor || q.JobID != "" && e.JobID != q.JobID || q.Type != "" && e.Type != q.Type || q.Actor != "" && e.Actor != q.Actor {
			continue
		}
		filtered = append(filtered, e)
	}
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 50
	}
	more := len(filtered) > q.Limit
	if more {
		filtered = filtered[:q.Limit]
	}
	p := EventPage{Events: filtered, ChainDigest: domain.Hash(jobEvents)}
	if len(filtered) > 0 {
		p.FirstDigest = filtered[0].Digest
		p.LastDigest = filtered[len(filtered)-1].Digest
		if more {
			p.NextCursor = filtered[len(filtered)-1].Sequence
		}
	}
	return p, nil
}
func (s *Store) EventDigest(id string) string { return domain.Hash(s.Events(id)) }
func (s *Store) appendLocked(typ, id string, v int64, actor string, data any) {
	b, _ := json.Marshal(data)
	s.seq++
	e := domain.Event{Sequence: s.seq, Type: typ, JobID: id, Version: v, At: time.Now().UTC(), Actor: actor, Data: b, PrevDigest: s.prev}
	e.Digest = domain.EventRecordDigest(e)
	s.prev = e.Digest
	s.events = append(s.events, e)
}
func clone(j *domain.ReviewJob) *domain.ReviewJob {
	return domain.CloneJob(j)
}
