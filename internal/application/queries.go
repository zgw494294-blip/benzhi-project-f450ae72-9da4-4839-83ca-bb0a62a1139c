package application

import (
	"caption-delivery-qc/internal/domain"
	"encoding/json"
	"sort"
	"time"
)

type Detail struct {
	Job        *domain.ReviewJob  `json:"job"`
	Credential *domain.Credential `json:"credential,omitempty"`
	Events     []domain.Event     `json:"events"`
}

type CandidateSnapshotView struct {
	JobID         string       `json:"jobID"`
	Revision      int          `json:"revision"`
	ReviewRound   int          `json:"reviewRound"`
	SubmittedBy   string       `json:"submittedBy"`
	SubmittedAt   time.Time    `json:"submittedAt"`
	ContentDigest string       `json:"contentDigest"`
	CueCount      int          `json:"cueCount"`
	Cues          []domain.Cue `json:"cues"`
}

func snapshotView(jobID string, snap domain.CandidateSnapshot) CandidateSnapshotView {
	cues := make([]domain.Cue, 0, len(snap.Cues))
	for _, cue := range snap.Cues {
		cues = append(cues, cue)
	}
	sort.Slice(cues, func(i, k int) bool {
		if cues[i].Sequence == cues[k].Sequence {
			return cues[i].ID < cues[k].ID
		}
		return cues[i].Sequence < cues[k].Sequence
	})
	return CandidateSnapshotView{JobID: jobID, Revision: snap.Revision, ReviewRound: snap.ReviewRound, SubmittedBy: snap.SubmittedBy, SubmittedAt: snap.SubmittedAt, ContentDigest: snap.ContentDigest, CueCount: len(cues), Cues: cues}
}

func cloneDetail(d Detail) Detail {
	out := d
	out.Job = domain.CloneJob(d.Job)
	out.Events = make([]domain.Event, len(d.Events))
	for i, event := range d.Events {
		out.Events[i] = event
		out.Events[i].Data = append(json.RawMessage(nil), event.Data...)
	}
	if d.Credential != nil {
		credential := *d.Credential
		out.Credential = &credential
	}
	return out
}

func (s *Service) Detail(id string) (Detail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.detailCache[id]; ok {
		return cloneDetail(cached), nil
	}
	j, e := s.Get(id)
	if e != nil {
		return Detail{}, e
	}
	ev, _ := s.Timeline(id)
	d := Detail{Job: j, Events: ev}
	if c, ok := s.store.Credential(id); ok {
		d.Credential = &c
	}
	s.detailCache[id] = cloneDetail(d)
	return cloneDetail(d), nil
}
