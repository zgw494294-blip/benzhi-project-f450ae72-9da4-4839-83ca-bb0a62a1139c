package idempotency_result_alias

import (
	"caption-delivery-qc/internal/application"
	"caption-delivery-qc/internal/domain"
	"caption-delivery-qc/internal/journal"
	"testing"
)

func TestCueImportIdempotencyReplayIsolatedFromResponseMutation(t *testing.T) {
	store, err := journal.New("")
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(store)
	job, err := service.CreateJob(application.CreateJobInput{
		ProgramTitle:   "公共广播节目",
		DurationMs:     5000,
		Language:       "zh-CN",
		DeliveryBatch:  "batch-1",
		RuleSet:        "broadcast-v1",
		Actor:          "producer",
		IdempotencyKey: "create-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	ops := []domain.CueEdit{{
		Op:    "add",
		CueID: "cue-1",
		Cue: domain.Cue{
			StartMs: 0,
			EndMs:   5000,
			Text:    "这是可交付字幕",
		},
	}}
	_, first, err := service.BatchCues(job.ID, ops, job.Version, "producer", "import-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].ContentDigest == "" {
		t.Fatalf("首次导入结果无效: %#v", first)
	}
	wantDigest := first[0].ContentDigest
	first[0].ContentDigest = "caller-mutated"

	_, replay, err := service.BatchCues(job.ID, ops, job.Version, "producer", "import-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(replay) != 1 || replay[0].ContentDigest != wantDigest {
		t.Fatalf("幂等重放受到首次响应修改污染: %#v", replay)
	}
	replay[0].ContentDigest = "replay-mutated"
	_, third, err := service.BatchCues(job.ID, ops, job.Version, "producer", "import-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(third) != 1 || third[0].ContentDigest != wantDigest {
		t.Fatalf("后续幂等重放共享响应切片: %#v", third)
	}
}
