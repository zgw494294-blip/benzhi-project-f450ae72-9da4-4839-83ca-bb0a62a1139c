package credentialeventdigestcache

import (
	"caption-delivery-qc/internal/application"
	"caption-delivery-qc/internal/domain"
	"caption-delivery-qc/internal/journal"
	"testing"
)

func TestEventDigestCacheInvalidatedAfterAppend(t *testing.T) {
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

	// The public timeline query warms the digest before the next journal append.
	if _, err := service.TimelinePage(job.ID, application.TimelineQuery{}); err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpsertCue(job.ID, domain.Cue{
		ID:      "cue-1",
		StartMs: 0,
		EndMs:   5000,
		Text:    "完整字幕",
	}, job.Version, "producer")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version == job.Version {
		t.Fatal("字幕追加未推进任务版本")
	}

	submitted, err := service.Submit(job.ID, updated.Version, "producer")
	if err != nil {
		t.Fatal(err)
	}
	_, findingDigest, err := service.FindingStatistics(job.ID, submitted.ReviewRound)
	if err != nil {
		t.Fatal(err)
	}
	finished, err := service.FinishReviewWithConclusion(job.ID, submitted.Version, "reviewer", "通过", findingDigest)
	if err != nil {
		t.Fatal(err)
	}
	checklist, checklistDigest, err := service.Checklist(job.ID, finished.Version, "lead")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.Approve(job.ID, finished.Version, "lead", checklist.CandidateRevision, checklist.CandidateDigest, checklistDigest, "签署")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := service.Credential(job.ID, approved.Version, "issuer", "credential-1")
	if err != nil {
		t.Fatal(err)
	}
	valid, _, err := service.VerifyCredential(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatalf("签证后事件链摘要未与当前日志一致: %s", credential.EventChainDigest)
	}
}
