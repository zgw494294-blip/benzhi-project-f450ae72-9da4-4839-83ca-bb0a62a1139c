package domain

import (
	"testing"
	"time"
)

func draftWithCue() *ReviewJob {
	j := NewJob("job", "节目", "zh-CN", "batch-1", "broadcast-v1", "producer", 5000, time.Unix(1, 0).UTC())
	j.Cues["cue-1"] = Cue{ID: "cue-1", JobID: j.ID, Sequence: 1, StartMs: 0, EndMs: 5000, Text: "完整字幕", ContentHash: Hash("完整字幕")}
	return j
}

func TestMetadataRevisionIsAtomic(t *testing.T) {
	j := draftWithCue()
	before := CloneJob(j)
	err := j.ReviseMetadata(MetadataRevision{ProgramTitle: "新节目", DurationMs: 4000, Language: "zh-CN", DeliveryBatch: "batch-2", RuleSet: "broadcast-v1"}, 0)
	if err == nil {
		t.Fatal("缩短时长导致字幕越界时应拒绝")
	}
	if j.Version != before.Version || j.ProgramTitle != before.ProgramTitle || j.DurationMs != before.DurationMs {
		t.Fatal("失败的元数据修订改变了聚合")
	}
	if err = j.ReviseMetadata(MetadataRevision{ProgramTitle: "新节目", DurationMs: 5000, Language: "zh-CN", DeliveryBatch: "batch-2", RuleSet: "broadcast-v2"}, 0); err != nil {
		t.Fatal(err)
	}
	if j.Version != 1 || j.DeliveryBatch != "batch-2" {
		t.Fatalf("元数据修订未生效: %#v", j.Metadata())
	}
}

func TestCuePreviewReportsAllQualityProblemsWithoutMutation(t *testing.T) {
	j := draftWithCue()
	before := CloneJob(j)
	preview := j.PreviewCueEdits([]CueEdit{{Op: "update", CueID: "cue-1", Cue: Cue{StartMs: 3000, EndMs: 3100, Text: "这是一条阅读速度明显超限的字幕"}}, {Op: "add", CueID: "cue-2", Cue: Cue{StartMs: 3000, EndMs: 5000, Text: "第二条"}}}, 0)
	if preview.Importable {
		t.Fatal("重叠且阅读速度超限的批次不应可导入")
	}
	codes := map[string]bool{}
	for _, issue := range preview.Issues {
		codes[issue.Code] = true
	}
	if !codes["READING_SPEED"] || !codes["OVERLAP"] {
		t.Fatalf("未同时返回质量问题: %#v", preview.Issues)
	}
	if j.Version != before.Version || j.Cues["cue-1"].Text != before.Cues["cue-1"].Text {
		t.Fatal("预检修改了任务投影")
	}
}

func TestSnapshotConclusionBaselineAndChecklist(t *testing.T) {
	j := draftWithCue()
	now := time.Unix(2, 0).UTC()
	if err := j.FreezeSubmit(0, "producer", now); err != nil {
		t.Fatal(err)
	}
	snapshot := j.Snapshots[1]
	if err := j.AddFinding(Finding{ID: "finding-1", CueID: "cue-1", Category: "正文", Severity: "high", Evidence: "证据", ReportedBy: "reviewer", ReportedAt: now}, j.Version); err != nil {
		t.Fatal(err)
	}
	version := j.Version
	if err := j.FinishReviewWithConclusion(version, "reviewer", "退回修订", "wrong", now); err == nil || j.Version != version || j.Status != StatusInReview {
		t.Fatal("错误统计摘要应保持审校状态和版本")
	}
	_, summaryDigest := j.FindingStatisticsDigest(j.ReviewRound)
	if err := j.FinishReviewWithConclusion(version, "reviewer", "退回修订", summaryDigest, now); err != nil {
		t.Fatal(err)
	}
	beforeText := snapshot.Cues["cue-1"].Text
	if _, err := j.ApplyReworkCueEdits([]CueEdit{{Op: "update", CueID: "cue-1", Cue: Cue{StartMs: 0, EndMs: 5000, Text: "修订后的完整字幕"}}}, j.Version, 1, "wrong", "producer", now); err == nil {
		t.Fatal("错误退修基线应拒绝")
	}
	if _, err := j.ApplyReworkCueEdits([]CueEdit{{Op: "update", CueID: "cue-1", Cue: Cue{StartMs: 0, EndMs: 5000, Text: "修订后的完整字幕"}}}, j.Version, 1, snapshot.ContentDigest, "producer", now); err != nil {
		t.Fatal(err)
	}
	historical, err := j.Snapshot(1, snapshot.ContentDigest)
	if err != nil || historical.Cues["cue-1"].Text != beforeText {
		t.Fatal("退修编辑改变了历史快照")
	}
	if err = j.ApplyFindingResponses([]FindingResponse{{FindingID: "finding-1", Note: "已修订", ResponseRevision: 2, CueContentHash: j.Cues["cue-1"].ContentHash}}, j.Version, now); err != nil {
		t.Fatal(err)
	}
	if err = j.FreezeSubmit(j.Version, "producer", now); err != nil {
		t.Fatal(err)
	}
	_, digest2 := j.FindingStatisticsDigest(j.ReviewRound)
	if err = j.FinishReviewWithConclusion(j.Version, "reviewer", "同意批准", digest2, now); err != nil {
		t.Fatal(err)
	}
	_, checklistDigest := j.ApprovalChecklist(j.Version, "lead")
	if err = j.ApproveWithChecklist(j.Version, "lead", j.CurrentRevision, j.Snapshots[j.CurrentRevision].ContentDigest, "wrong", "签署", now); err == nil || j.Status != StatusPendingApproval {
		t.Fatal("错误清单摘要应保持待批准状态")
	}
	if err = j.ApproveWithChecklist(j.Version, "lead", j.CurrentRevision, j.Snapshots[j.CurrentRevision].ContentDigest, checklistDigest, "签署", now); err != nil {
		t.Fatal(err)
	}
}
