package journal_startup_resource_loss

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"caption-delivery-qc/internal/domain"
	"caption-delivery-qc/internal/journal"
)

func TestJournalStartupRejectsUnreadableConfiguredPath(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "journal-state")
	store, err := journal.New(statePath)
	if err != nil {
		t.Fatal(err)
	}
	job := domain.NewJob("persisted-job", "节目", "zh-CN", "batch-1", "broadcast-v1", "producer", 1000, time.Unix(1, 0).UTC())
	if err := store.Create(job, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0700); err != nil {
		t.Fatal(err)
	}

	store, err = journal.New(statePath)
	if err == nil {
		if _, getErr := store.Get("persisted-job"); getErr != domain.ErrNotFound {
			t.Fatalf("unexpected lookup result: %v", getErr)
		}
		t.Fatalf("journal.New accepted an unreadable configured state path")
	}
}
