package journal

import "caption-delivery-qc/internal/domain"

func (s *Store) Verify() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	if !s.Verify() {
		return domain.ErrIntegrity
	}
	return nil
}
