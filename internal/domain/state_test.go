package domain

import (
	"testing"
	"time"
)

func TestReviewLifecycle(t *testing.T) {
	j := NewJob("j", "节目", "zh-CN", "b", "broadcast-v1", "producer", 5000, time.Now())
	j.Cues["c"] = Cue{ID: "c", StartMs: 0, EndMs: 5000, Text: "测试字幕"}
	if err := j.FreezeSubmit(0, "producer", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := j.AddFinding(Finding{ID: "f", CueID: "c", Category: "错字", Severity: "blocking", Evidence: "证据"}, j.Version); err != nil {
		t.Fatal(err)
	}
	if err := j.FinishReview(j.Version, "reviewer"); err != nil || j.Status != StatusRework {
		t.Fatalf("finish: %v %s", err, j.Status)
	}
	if err := j.RespondFinding("f", "已修订", 2, j.Version, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := j.FreezeSubmit(j.Version, "producer", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := j.FinishReview(j.Version, "reviewer"); err != nil || j.Status != StatusPendingApproval {
		t.Fatalf("finish2: %v %s", err, j.Status)
	}
	if err := j.Approve(j.Version, "lead", time.Now()); err != nil || j.Status != StatusApproved {
		t.Fatalf("approve: %v %s", err, j.Status)
	}
}
