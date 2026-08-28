package cueimportalias

import (
	"caption-delivery-qc/internal/application"
	"caption-delivery-qc/internal/domain"
	"caption-delivery-qc/internal/journal"
	"testing"
)

func TestCueImportReplayDoesNotDependOnMutatedCallerInput(t *testing.T) {
	store, err := journal.New("")
	if err != nil {
		t.Fatal(err)
	}
	app := application.New(store)
	job, err := app.CreateJob(application.CreateJobInput{
		ProgramTitle: "晚间新闻", DurationMs: 10000, Language: "zh-CN",
		DeliveryBatch: "batch-1", RuleSet: "broadcast-v1", Actor: "producer",
		IdempotencyKey: "create-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	ops := []domain.CueEdit{{
		Action: "add", CueID: "cue-1",
		Cue: domain.Cue{StartMs: 0, EndMs: 2000, Speaker: "主持人", Text: "本条字幕用于幂等重放测试"},
	}}
	if _, _, err = app.BatchCues(job.ID, ops, 0, "producer", "import-1"); err != nil {
		t.Fatal(err)
	}
	if ops[0].Op != "" {
		t.Errorf("caller operation was mutated: op=%q", ops[0].Op)
	}
	if _, _, err = app.BatchCues(job.ID, ops, 0, "producer", "import-1"); err != nil {
		t.Fatalf("idempotent replay rejected reused request: %v", err)
	}
}
