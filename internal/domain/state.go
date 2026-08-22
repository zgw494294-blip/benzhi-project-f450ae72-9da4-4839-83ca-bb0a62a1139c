package domain

import (
	"fmt"
	"strings"
	"time"
)

func NewJob(id, title, lang, batch, rules, actor string, duration int64, now time.Time) *ReviewJob {
	return &ReviewJob{ID: id, ProgramTitle: title, Language: lang, DeliveryBatch: batch, RuleSet: rules, DurationMs: duration, Status: StatusDraft, CreatedBy: actor, CreatedAt: now, Cues: map[string]Cue{}, Findings: map[string]Finding{}, Snapshots: map[int]CandidateSnapshot{}}
}
func (j *ReviewJob) FreezeSubmit(expected int64, actor string, now time.Time) error {
	if j.Version != expected {
		return ErrConflict
	}
	if j.Status != StatusDraft && j.Status != StatusRework {
		return ErrInvalidState
	}
	pre := j.SubmissionPreflight(expected)
	if !pre.CanSubmit {
		if len(pre.Issues) > 0 {
			return fmt.Errorf("%w:%s", ErrValidation, pre.Issues[0].Message)
		}
		return ErrValidation
	}
	j.CurrentRevision++
	j.ReviewRound++
	j.Status = StatusInReview
	j.SubmittedBy = actor
	j.ReviewFinishedBy = ""
	if j.Snapshots == nil {
		j.Snapshots = map[int]CandidateSnapshot{}
	}
	j.Snapshots[j.CurrentRevision] = CandidateSnapshot{Revision: j.CurrentRevision, ReviewRound: j.ReviewRound, SubmittedBy: actor, SubmittedAt: now, ContentDigest: j.ContentDigest(), Cues: cloneCues(j.Cues)}
	j.Version++
	return nil
}
func (j *ReviewJob) AddFinding(f Finding, expected int64) error {
	if IsImmutable(j.Status) {
		return ErrImmutableConflict
	}
	if j.Version != expected {
		return ErrConflict
	}
	if j.Status != StatusInReview {
		return ErrInvalidState
	}
	if f.Evidence == "" || f.Category == "" || f.Severity == "" {
		return fmt.Errorf("%w:问题字段不完整", ErrValidation)
	}
	switch f.Severity {
	case "blocking", "high", "medium", "low":
	default:
		return fmt.Errorf("%w:问题严重级别无效", ErrValidation)
	}
	if _, ok := j.Cues[f.CueID]; !ok {
		return fmt.Errorf("%w:字幕段不存在", ErrValidation)
	}
	if _, exists := j.Findings[f.ID]; exists {
		return fmt.Errorf("%w:问题标识重复", ErrValidation)
	}
	f.JobID = j.ID
	f.Category = strings.TrimSpace(f.Category)
	f.Severity = strings.TrimSpace(f.Severity)
	f.Evidence = strings.Join(strings.Fields(f.Evidence), " ")
	f.ReviewRound = j.ReviewRound
	f.CandidateRevision = j.CurrentRevision
	f.Status = "open"
	f.Fingerprint = FindingFingerprint(f.CueID, f.Category, f.Severity, f.Evidence)
	if f.PreviousFindingID != "" {
		prev, ok := j.Findings[f.PreviousFindingID]
		if !ok || prev.Status != "resolved" || prev.Fingerprint != f.Fingerprint {
			return fmt.Errorf("%w:历史问题关联无效", ErrValidation)
		}
	}
	for _, existing := range j.Findings {
		if existing.CandidateRevision == f.CandidateRevision && existing.Fingerprint == f.Fingerprint {
			return fmt.Errorf("%w:同一候选版本的问题重复", ErrValidation)
		}
		if f.PreviousFindingID == "" && existing.Fingerprint == f.Fingerprint && existing.Status == "resolved" && existing.CandidateRevision < f.CandidateRevision {
			f.PreviousFindingID = existing.ID
		}
	}
	j.Findings[f.ID] = f
	j.Version++
	return nil
}
func (j *ReviewJob) FinishReview(expected int64, actor string) error {
	_, digest := j.FindingStatisticsDigest(j.ReviewRound)
	return j.FinishReviewWithConclusion(expected, actor, "兼容流程审校结论", digest, time.Now().UTC())
}
func (j *ReviewJob) RespondFinding(id, note string, revision int, expected int64, now time.Time) error {
	if revision == 0 {
		revision = j.CurrentRevision + 1
	}
	return j.ApplyFindingResponses([]FindingResponse{{FindingID: id, Note: note, ResponseRevision: revision}}, expected, now)
}
func (j *ReviewJob) Approve(expected int64, reviewer string, now time.Time, evidence ...any) error {
	if IsImmutable(j.Status) {
		return ErrImmutableConflict
	}
	if j.Version != expected {
		return ErrConflict
	}
	report := j.ApprovalReadiness(expected, j.CurrentRevision, reviewer)
	revision, digest, note := j.CurrentRevision, j.ContentDigest(), ""
	if len(evidence) > 0 {
		if v, ok := evidence[0].(int); ok && v > 0 {
			revision = v
		}
	}
	if len(evidence) > 1 {
		if v, ok := evidence[1].(string); ok && v != "" {
			digest = v
		}
	}
	if len(evidence) > 2 {
		if v, ok := evidence[2].(string); ok {
			note = v
		}
	}
	if len(evidence) == 0 {
		note = "legacy approval"
	}
	if note == "" {
		return fmt.Errorf("%w:签字备注不能为空", ErrValidation)
	}
	if revision != j.CurrentRevision || digest != j.Snapshots[j.CurrentRevision].ContentDigest {
		return fmt.Errorf("%w:候选摘要不匹配", ErrValidation)
	}
	for _, check := range report.Checks {
		if check.Status == "blocking" {
			switch check.Code {
			case "VERSION_MISMATCH":
				return ErrConflict
			case "DUTY_SEPARATION":
				return ErrForbidden
			case "STATUS_NOT_READY":
				return ErrInvalidState
			default:
				return fmt.Errorf("%w:%s", ErrValidation, check.Message)
			}
		}
	}
	j.Status = StatusApproved
	j.ApprovedBy = reviewer
	j.ApprovedAt = &now
	j.ApprovalRevision, j.ApprovalDigest, j.ApprovalNote = revision, digest, note
	j.Version++
	return nil
}
func (j *ReviewJob) Credential(expected int64, issuer string, now time.Time, eventDigest string) (*Credential, error) {
	if j.Version != expected {
		return nil, ErrConflict
	}
	if j.Status != StatusApproved {
		return nil, ErrInvalidState
	}
	if issuer == j.CreatedBy {
		return nil, ErrForbidden
	}
	c := &Credential{ID: j.ID + "-cred-" + fmt.Sprint(j.CurrentRevision), JobID: j.ID, Revision: j.CurrentRevision, ContentDigest: j.ContentDigest(), EventChainDigest: eventDigest, ApprovedBy: j.ApprovedBy, IssuedBy: issuer, IssuedAt: now, Algorithm: "SHA-256"}
	c.CredentialDigest = Hash(c)
	j.Status = StatusCredentialed
	j.Version++
	return c, nil
}

func cloneCues(cues map[string]Cue) map[string]Cue {
	out := make(map[string]Cue, len(cues))
	for id, cue := range cues {
		out[id] = cue
	}
	return out
}
