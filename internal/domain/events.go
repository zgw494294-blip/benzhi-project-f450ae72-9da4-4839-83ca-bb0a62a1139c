package domain

import "time"

type EventName string

const (
	EventJobCreated     EventName = "job.created"
	EventCueUpsert      EventName = "cue.upsert"
	EventCueDelete      EventName = "cue.delete"
	EventSubmitted      EventName = "review.submitted"
	EventFinding        EventName = "finding.reported"
	EventReviewFinished EventName = "review.finished"
	EventResponse       EventName = "finding.responded"
	EventApproved       EventName = "job.approved"
	EventCredential     EventName = "credential.issued"
)

type AuditEntry struct {
	EventType EventName `json:"eventType"`
	Actor     string    `json:"actor"`
	At        time.Time `json:"at"`
	Summary   string    `json:"summary"`
}
