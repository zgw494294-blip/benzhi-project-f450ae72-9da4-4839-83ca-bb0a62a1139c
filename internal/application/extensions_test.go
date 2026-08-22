package application

import (
	"caption-delivery-qc/internal/domain"
	"caption-delivery-qc/internal/journal"
	"errors"
	"testing"
)

func TestMetadataIdempotencyAndPreviewAreJournalSafe(t *testing.T) {
	store, err := journal.New("")
	if err != nil {
		t.Fatal(err)
	}
	service := New(store)
	job, err := service.CreateJob(CreateJobInput{ProgramTitle: "节目", DurationMs: 5000, Language: "zh-CN", DeliveryBatch: "batch-1", RuleSet: "broadcast-v1", Actor: "producer", IdempotencyKey: "create"})
	if err != nil {
		t.Fatal(err)
	}
	next := job.Metadata()
	next.DeliveryBatch = "batch-2"
	updated, err := service.ReviseMetadata(job.ID, next, job.Version, "producer", "metadata-1")
	if err != nil {
		t.Fatal(err)
	}
	eventCount := len(store.Events(job.ID))
	replayed, err := service.ReviseMetadata(job.ID, next, job.Version, "producer", "metadata-1")
	if err != nil || replayed.Version != updated.Version || len(store.Events(job.ID)) != eventCount {
		t.Fatalf("元数据幂等重放失败: %v", err)
	}
	changed := next
	changed.Language = "en-US"
	if _, err = service.ReviseMetadata(job.ID, changed, job.Version, "producer", "metadata-1"); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("同键不同参数应冲突: %v", err)
	}
	before, _ := service.Get(job.ID)
	preview, err := service.PreviewCues(job.ID, []domain.CueEdit{{Op: "add", CueID: "cue-1", Cue: domain.Cue{StartMs: 0, EndMs: 5000, Text: "字幕"}}}, before.Version)
	if err != nil || !preview.Importable {
		t.Fatalf("合法批次预检失败: %#v %v", preview, err)
	}
	after, _ := service.Get(job.ID)
	if after.Version != before.Version || len(store.Events(job.ID)) != eventCount {
		t.Fatal("字幕预检写入了投影或事件")
	}
}
