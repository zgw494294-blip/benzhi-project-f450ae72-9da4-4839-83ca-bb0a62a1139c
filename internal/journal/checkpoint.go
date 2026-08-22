package journal

import (
	"caption-delivery-qc/internal/domain"
	"encoding/json"
	"os"
)

func (s *Store) Verify() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.verifyLocked()
}

func (s *Store) verifyLocked() bool {
	prev := ""
	var seq int64
	for _, e := range s.events {
		if (seq != 0 && e.Sequence != seq+1) || e.PrevDigest != prev || e.Digest != domain.EventRecordDigest(e) {
			return false
		}
		seq = e.Sequence
		prev = e.Digest
	}
	return seq == s.seq && prev == s.prev
}
func (s *Store) Snapshot(id string) (*domain.ReviewJob, error) { return s.Get(id) }
func (s *Store) VerifyIntegrity() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.verifyLocked() {
		return domain.ErrIntegrity
	}
	if s.path == "" {
		return nil
	}
	if s.integrityFile == nil {
		f, err := os.Open(s.path)
		if err != nil {
			return err
		}
		s.integrityFile = f
	}
	if _, err := s.integrityFile.Seek(0, 0); err != nil {
		return err
	}
	var persisted persistedState
	if err := json.NewDecoder(s.integrityFile).Decode(&persisted); err != nil {
		return err
	}
	if persisted.Sequence != s.seq || persisted.PreviousDigest != s.prev || !Replay(persisted.Events) {
		return domain.ErrIntegrity
	}
	return nil
}
