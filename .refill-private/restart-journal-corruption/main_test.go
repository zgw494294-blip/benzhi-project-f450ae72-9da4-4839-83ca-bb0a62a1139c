package restartjournalcorruption

import (
	"caption-delivery-qc/internal/application"
	"caption-delivery-qc/internal/domain"
	"caption-delivery-qc/internal/journal"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRestartRejectsCorruptJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := journal.New(path)
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(store)
	job, err := service.CreateJob(application.CreateJobInput{
		ProgramTitle: "重启校验节目", DurationMs: 5000, Language: "zh-CN",
		DeliveryBatch: "batch-1", RuleSet: "broadcast-v1", Actor: "producer", IdempotencyKey: "create-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = service.UpsertCue(job.ID, domain.Cue{ID: "cue-1", StartMs: 0, EndMs: 2000, Text: "第一段"}, job.Version, "producer")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.UpsertCue(job.ID, domain.Cue{ID: "cue-2", StartMs: 2500, EndMs: 5000, Text: "第二段"}, job.Version, "producer")
	if err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(contents, &persisted); err != nil {
		t.Fatal(err)
	}
	events, ok := persisted["events"].([]any)
	if !ok || len(events) != 3 {
		t.Fatalf("预期三条事件，得到 %#v", persisted["events"])
	}
	middle, ok := events[1].(map[string]any)
	if !ok {
		t.Fatal("中间事件格式无效")
	}
	middle["type"] = "tampered.middle"
	corrupted, err := json.Marshal(persisted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, corrupted, 0600); err != nil {
		t.Fatal(err)
	}

	restarted, err := journal.New(path)
	if err == nil {
		t.Fatalf("中间事件损坏后重启不应接受截断日志：events=%d verify=%v", len(restarted.Events(job.ID)), restarted.VerifyIntegrity())
	}
	if !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("中间事件损坏应返回 domain.ErrIntegrity，得到 %v", err)
	}
}
