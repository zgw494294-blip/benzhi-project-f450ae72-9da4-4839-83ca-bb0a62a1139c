package credentialcommitrollback_test

import (
	"caption-delivery-qc/internal/application"
	"caption-delivery-qc/internal/domain"
	"caption-delivery-qc/internal/journal"
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialCommitFailureRollsBackState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	store, err := journal.New(statePath)
	if err != nil {
		t.Fatal(err)
	}
	job := &domain.ReviewJob{
		ID:              "job-rollback",
		ProgramTitle:    "晚间新闻",
		DurationMs:      1000,
		Language:        "zh",
		DeliveryBatch:   "batch-1",
		RuleSet:         "broadcast-v1",
		Status:          domain.StatusApproved,
		CurrentRevision: 1,
		Version:         1,
		CreatedBy:       "maker",
		ApprovedBy:      "reviewer",
		Cues:            map[string]domain.Cue{},
		Findings:        map[string]domain.Finding{},
		Snapshots:       map[int]domain.CandidateSnapshot{1: {Revision: 1, ContentDigest: domain.Hash("")}},
	}
	if err := store.Create(job, ""); err != nil {
		t.Fatal(err)
	}

	// Replacing the file with a directory makes the temporary-file rename fail
	// after CommitCredential has updated its in-memory projections.
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0700); err != nil {
		t.Fatal(err)
	}

	service := application.New(store)
	_, err = service.Credential(job.ID, job.Version, "issuer", "issue-1")
	if err == nil {
		t.Fatal("expected persistence failure")
	}
	if _, found := store.Credential(job.ID); found {
		t.Fatalf("credential remained in memory after failed commit: %v", err)
	}
	if _, retryErr := service.Credential(job.ID, job.Version, "issuer", "issue-1"); retryErr == nil {
		t.Fatal("retry unexpectedly succeeded after the failed commit")
	}
}
