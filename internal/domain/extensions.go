package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func (j *ReviewJob) Metadata() MetadataRevision {
	return MetadataRevision{ProgramTitle: j.ProgramTitle, DurationMs: j.DurationMs, Language: j.Language, DeliveryBatch: j.DeliveryBatch, RuleSet: j.RuleSet}
}

func (j *ReviewJob) ReviseMetadata(next MetadataRevision, expected int64) error {
	if j.Version != expected {
		return ErrConflict
	}
	if j.Status != StatusDraft {
		return ErrInvalidState
	}
	if err := ValidateMetadata(next.ProgramTitle, next.Language, next.DeliveryBatch, next.RuleSet, next.DurationMs); err != nil {
		return err
	}
	copyJob := CloneJob(j)
	copyJob.ProgramTitle, copyJob.DurationMs, copyJob.Language = next.ProgramTitle, next.DurationMs, next.Language
	copyJob.DeliveryBatch, copyJob.RuleSet = next.DeliveryBatch, next.RuleSet
	if len(copyJob.Cues) > 0 {
		if err := copyJob.ValidateCues(); err != nil {
			return fmt.Errorf("%w:元数据修订会使字幕无效", ErrValidation)
		}
	}
	if _, valid := RuleSetGapThreshold(next.RuleSet); !valid {
		return fmt.Errorf("%w:规则集无效", ErrValidation)
	}
	copyJob.Version++
	*j = *copyJob
	return nil
}

func (j *ReviewJob) Snapshot(revision int, digest string) (CandidateSnapshot, error) {
	snap, ok := j.Snapshots[revision]
	if !ok {
		return CandidateSnapshot{}, ErrSnapshotNotFrozen
	}
	if digest != "" && digest != snap.ContentDigest {
		return CandidateSnapshot{}, ErrCandidateDigest
	}
	snap.Cues = cloneCues(snap.Cues)
	ids := make([]string, 0, len(snap.Cues))
	for id := range snap.Cues {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, k int) bool {
		a, b := snap.Cues[ids[i]], snap.Cues[ids[k]]
		if a.Sequence == b.Sequence {
			return ids[i] < ids[k]
		}
		return a.Sequence < b.Sequence
	})
	ordered := make(map[string]Cue, len(ids))
	for _, id := range ids {
		ordered[id] = snap.Cues[id]
	}
	snap.Cues = ordered
	return snap, nil
}

func (j *ReviewJob) FindingStatistics(round int) FindingStatistics {
	stats := FindingStatistics{ReviewRound: round}
	for _, f := range j.Findings {
		if round > 0 && f.ReviewRound != round {
			continue
		}
		stats.Total++
		if f.Status != "resolved" && (f.Severity == "blocking" || f.Severity == "high") {
			stats.OpenBlocking++
		}
		switch f.Severity {
		case "blocking":
			stats.BySeverity.Blocking++
		case "high":
			stats.BySeverity.High++
		case "medium":
			stats.BySeverity.Medium++
		case "low":
			stats.BySeverity.Low++
		}
	}
	return stats
}

func (j *ReviewJob) FindingStatisticsDigest(round int) (FindingStatistics, string) {
	stats := j.FindingStatistics(round)
	return stats, Hash(stats)
}

func (j *ReviewJob) FinishReviewWithConclusion(expected int64, actor, note, digest string, now time.Time) error {
	if j.Version != expected {
		return ErrConflict
	}
	if j.Status != StatusInReview {
		return ErrInvalidState
	}
	if strings.TrimSpace(actor) == "" || actor == "anonymous" || actor == j.CreatedBy || actor == j.SubmittedBy {
		return ErrForbidden
	}
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("%w:结论说明不能为空", ErrValidation)
	}
	stats, actual := j.FindingStatisticsDigest(j.ReviewRound)
	if digest == "" || digest != actual {
		return fmt.Errorf("%w:问题统计摘要不匹配", ErrValidation)
	}
	if len(j.ReviewConclusions) > 0 && j.ReviewConclusions[len(j.ReviewConclusions)-1].ReviewRound == j.ReviewRound {
		return fmt.Errorf("%w:当前轮次已完成审校", ErrValidation)
	}
	status := StatusPendingApproval
	if stats.OpenBlocking > 0 {
		status = StatusRework
	}
	j.ReviewConclusions = append(j.ReviewConclusions, ReviewConclusion{ReviewRound: j.ReviewRound, Reviewer: actor, ConclusionNote: strings.TrimSpace(note), FindingSummary: stats, FindingSummaryDigest: actual, FinishedAt: now, ResultStatus: status})
	j.Status = status
	j.ReviewFinishedBy = actor
	j.Version++
	return nil
}

func (j *ReviewJob) ApprovalChecklist(expected int64, actor string) (ApprovalChecklistSnapshot, string) {
	report := j.ApprovalReadiness(expected, j.CurrentRevision, actor)
	checks := report.Checks
	stats := j.FindingStatistics(j.ReviewRound)
	digest := ""
	if len(j.ReviewConclusions) > 0 {
		last := j.ReviewConclusions[len(j.ReviewConclusions)-1]
		if last.ReviewRound == j.ReviewRound {
			digest = last.FindingSummaryDigest
		}
	}
	s := ApprovalChecklistSnapshot{ChecklistVersion: 1, CandidateRevision: j.CurrentRevision, CandidateDigest: j.Snapshots[j.CurrentRevision].ContentDigest, ConclusionSummaryDigest: digest, OpenBlocking: stats.OpenBlocking, DutySeparation: actor != j.CreatedBy && actor != j.SubmittedBy && actor != j.ReviewFinishedBy, Checks: checks}
	return s, report.ChecklistDigest
}

func (j *ReviewJob) ApproveWithChecklist(expected int64, reviewer string, candidateRevision int, candidateDigest, checklistDigest, note string, now time.Time) error {
	if IsImmutable(j.Status) {
		return ErrImmutableConflict
	}
	if j.Version != expected {
		return ErrConflict
	}
	if j.Status != StatusPendingApproval {
		return ErrInvalidState
	}
	if candidateRevision != j.CurrentRevision {
		return ErrConflict
	}
	snap, ok := j.Snapshots[candidateRevision]
	if !ok {
		return ErrSnapshotNotFrozen
	}
	if candidateDigest != snap.ContentDigest {
		return ErrCandidateDigest
	}
	checklist, actual := j.ApprovalChecklist(expected, reviewer)
	if checklistDigest == "" || checklistDigest != actual {
		return ErrChecklistDigest
	}
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("%w:签字备注不能为空", ErrValidation)
	}
	for _, c := range checklist.Checks {
		if c.Status != "blocking" {
			continue
		}
		switch c.Code {
		case "DUTY_SEPARATION":
			return ErrForbidden
		case "VERSION_MISMATCH":
			return ErrConflict
		default:
			return fmt.Errorf("%w:%s", ErrValidation, c.Message)
		}
	}
	j.Status = StatusApproved
	j.ApprovedBy, j.ApprovedAt = reviewer, &now
	j.ApprovalRevision, j.ApprovalDigest, j.ApprovalNote = candidateRevision, candidateDigest, strings.TrimSpace(note)
	j.ApprovalChecklistDigest, j.ApprovalChecklistSnapshot = actual, &checklist
	j.Version++
	return nil
}

func (j *ReviewJob) ApplyReworkCueEdits(ops []CueEdit, expected int64, baseRevision int, baseDigest, actor string, now time.Time) ([]CueEditResult, error) {
	if IsImmutable(j.Status) {
		return nil, ErrImmutableConflict
	}
	if j.Status != StatusRework {
		return nil, ErrInvalidState
	}
	if strings.TrimSpace(actor) == "" || actor == "anonymous" || actor == j.ReviewFinishedBy {
		return nil, ErrForbidden
	}
	snap, ok := j.Snapshots[baseRevision]
	if baseRevision != j.CurrentRevision || !ok || snap.ContentDigest != baseDigest {
		return nil, ErrBaselineConflict
	}
	before := CloneJob(j)
	results, err := j.ApplyCueEdits(ops, expected)
	if err != nil {
		return nil, err
	}
	changed := make([]string, 0, len(ops))
	for _, op := range ops {
		id := op.CueID
		if id == "" {
			id = op.Cue.ID
		}
		old, hadOld := before.Cues[id]
		newCue, hadNew := j.Cues[id]
		if op.Op == "delete" || (hadOld && !hadNew) || (!hadOld && hadNew) || old.ContentHash != newCue.ContentHash {
			changed = append(changed, id)
		}
	}
	sort.Strings(changed)
	j.ReworkEdits = append(j.ReworkEdits, ReworkEditProof{BaseRevision: baseRevision, BaseDigest: baseDigest, ChangedCueIDs: changed, WorkVersion: j.Version, Actor: actor, EditedAt: now})
	return results, nil
}
