package timeline_event_alias

import (
	"caption-delivery-qc/internal/application"
	"caption-delivery-qc/internal/journal"
	"testing"
)

func TestTimelineEventMutationDoesNotCorruptJournal(t *testing.T) {
	store, err := journal.New("")
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(store)
	job, err := service.CreateJob(application.CreateJobInput{
		ProgramTitle:   "晨间新闻",
		DurationMs:     5000,
		Language:       "zh-CN",
		DeliveryBatch:  "batch-1",
		RuleSet:        "broadcast-v1",
		Actor:          "producer",
		IdempotencyKey: "timeline-alias-create",
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := service.Timeline(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	events[0].Type = "tampered"
	if _, err := service.TimelinePage(job.ID, application.TimelineQuery{}); err != nil {
		t.Fatalf("timeline read must not corrupt the journal: %v", err)
	}
}
