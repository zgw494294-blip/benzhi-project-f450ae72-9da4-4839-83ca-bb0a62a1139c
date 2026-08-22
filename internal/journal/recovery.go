package journal

import "caption-delivery-qc/internal/domain"

func Replay(events []domain.Event) bool {
	prev := ""
	var seq int64
	for _, e := range events {
		if (seq != 0 && e.Sequence != seq+1) || e.PrevDigest != prev || e.Digest != domain.EventRecordDigest(e) {
			return false
		}
		seq = e.Sequence
		prev = e.Digest
	}
	return true
}
func (s *Store) CheckpointValid() bool { return s.Verify() }
