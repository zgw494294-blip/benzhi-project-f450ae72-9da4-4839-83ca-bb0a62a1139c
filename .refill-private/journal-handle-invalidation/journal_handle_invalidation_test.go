package journal_handle_invalidation_test

import (
	"caption-delivery-qc/internal/application"
	"caption-delivery-qc/internal/domain"
	"caption-delivery-qc/internal/journal"
	"path/filepath"
	"testing"
)

func TestIntegrityHandleTracksAtomicJournalReplacement(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "journal.json")
	store, err := journal.New(statePath)
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(store)
	job, err := service.CreateJob(application.CreateJobInput{
		ProgramTitle: "晚间新闻", DurationMs: 5000, Language: "zh-CN",
		DeliveryBatch: "batch-handle", RuleSet: "broadcast-v1",
		Actor: "producer", IdempotencyKey: "create-handle",
	})
	if err != nil {
		t.Fatal(err)
	}

	add := []domain.CueEdit{{
		Op: "add", CueID: "cue-1",
		Cue: domain.Cue{ID: "cue-1", StartMs: 0, EndMs: 2000, Speaker: "主持人", Text: "这里是晚间新闻"},
	}}
	if _, err = service.PreviewCues(job.ID, add, job.Version); err != nil {
		t.Fatalf("initial integrity check failed: %v", err)
	}
	updated, _, err := service.BatchCues(job.ID, add, job.Version, "producer", "import-handle")
	if err != nil {
		t.Fatal(err)
	}

	update := []domain.CueEdit{{
		Op: "update", CueID: "cue-1",
		Cue: domain.Cue{ID: "cue-1", StartMs: 0, EndMs: 2000, Speaker: "主持人", Text: "这里是晚间新闻直播"},
	}}
	if _, err = service.PreviewCues(job.ID, update, updated.Version); err != nil {
		t.Fatalf("stale integrity handle rejected a valid journal: %v", err)
	}
}
