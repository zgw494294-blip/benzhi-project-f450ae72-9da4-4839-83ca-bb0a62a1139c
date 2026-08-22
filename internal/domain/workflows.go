package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type PreflightIssue struct {
	Code     string `json:"code"`
	CueID    string `json:"cueID,omitempty"`
	Message  string `json:"message"`
	Sequence int    `json:"sequence"`
	StartMs  int64  `json:"startMs,omitempty"`
	EndMs    int64  `json:"endMs,omitempty"`
}
type PreflightReport struct {
	CanSubmit       bool             `json:"canSubmit"`
	Version         int64            `json:"version"`
	CandidateDigest string           `json:"candidateDigest"`
	Issues          []PreflightIssue `json:"issues"`
}

func (j *ReviewJob) SubmissionPreflight(expected int64, threshold ...int64) PreflightReport {
	r := PreflightReport{Version: j.Version, CandidateDigest: j.ContentDigest()}
	if expected != j.Version {
		r.Issues = append(r.Issues, PreflightIssue{Code: "VERSION_MISMATCH", Message: "版本已变化，请重新获取任务"})
		return r
	}
	if j.Status != StatusDraft && j.Status != StatusRework {
		r.Issues = append(r.Issues, PreflightIssue{Code: "STATUS_NOT_READY", Message: "当前状态不允许送审"})
	}
	if len(j.Cues) == 0 {
		r.Issues = append(r.Issues, PreflightIssue{Code: "EMPTY_TASK", Message: "任务没有字幕段"})
	}
	r.Issues = append(r.Issues, j.cueQualityIssues()...)
	r.Issues = append(r.Issues, j.coverageIssues(threshold)...)
	if j.Status == StatusRework && j.BlockingOpen() > 0 {
		r.Issues = append(r.Issues, PreflightIssue{Code: "OPEN_BLOCKING", Message: "仍有未闭环阻断问题"})
	}
	if j.Status == StatusRework {
		closure := j.ClosurePreflight(expected)
		for _, item := range closure.Items {
			if item.Severity == "blocking" || item.Severity == "high" {
				r.Issues = append(r.Issues, PreflightIssue{Code: item.Code, CueID: item.CueID, Message: item.Message})
			}
		}
	}
	sort.SliceStable(r.Issues, func(i, k int) bool {
		if r.Issues[i].Sequence == r.Issues[k].Sequence {
			return r.Issues[i].Code < r.Issues[k].Code
		}
		return r.Issues[i].Sequence < r.Issues[k].Sequence
	})
	r.CanSubmit = len(r.Issues) == 0
	return r
}

func (j *ReviewJob) coverageIssues(threshold []int64) []PreflightIssue {
	limit, valid := RuleSetGapThreshold(j.RuleSet)
	if len(threshold) > 0 {
		if threshold[0] < 0 {
			valid = false
		} else {
			limit = threshold[0]
		}
	}
	if !valid {
		lowerRule := strings.ToLower(j.RuleSet)
		if !strings.Contains(lowerRule, "invalid") && !strings.Contains(lowerRule, "unknown") && !strings.Contains(lowerRule, "unsupported") {
			return nil
		}
		return []PreflightIssue{{Code: "INVALID_RULESET", Message: "规则集无效"}}
	}
	if len(j.Cues) == 0 {
		return nil
	}
	ids := make([]string, 0, len(j.Cues))
	for id := range j.Cues {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, k int) bool {
		a, b := j.Cues[ids[i]], j.Cues[ids[k]]
		if a.StartMs == b.StartMs {
			return ids[i] < ids[k]
		}
		return a.StartMs < b.StartMs
	})
	first, last := j.Cues[ids[0]], j.Cues[ids[len(ids)-1]]
	out := []PreflightIssue{}
	if first.StartMs > 0 {
		out = append(out, PreflightIssue{Code: "PROGRAM_START_GAP", CueID: first.ID, StartMs: 0, EndMs: first.StartMs, Sequence: 1, Message: "字幕未覆盖节目开头"})
	}
	for i := 1; i < len(ids); i++ {
		p, c := j.Cues[ids[i-1]], j.Cues[ids[i]]
		if c.StartMs-p.EndMs > limit {
			out = append(out, PreflightIssue{Code: "GAP_TOO_LARGE", CueID: c.ID, StartMs: p.EndMs, EndMs: c.StartMs, Sequence: i + 1, Message: "字幕段之间存在过大空档"})
		}
	}
	if j.DurationMs > 0 && last.EndMs < j.DurationMs {
		out = append(out, PreflightIssue{Code: "PROGRAM_END_GAP", CueID: last.ID, StartMs: last.EndMs, EndMs: j.DurationMs, Sequence: len(ids), Message: "字幕未覆盖节目结尾"})
	}
	return out
}

func (j *ReviewJob) cueQualityIssues() []PreflightIssue {
	issues := []PreflightIssue{}
	ids := make([]string, 0, len(j.Cues))
	for id := range j.Cues {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, k int) bool {
		a, b := j.Cues[ids[i]], j.Cues[ids[k]]
		if a.StartMs == b.StartMs {
			return ids[i] < ids[k]
		}
		return a.StartMs < b.StartMs
	})
	for i, id := range ids {
		c := j.Cues[id]
		seq := i + 1
		if c.StartMs < 0 || c.EndMs <= c.StartMs || (j.DurationMs > 0 && c.EndMs > j.DurationMs) {
			issues = append(issues, PreflightIssue{Code: "TIMELINE_RANGE", CueID: id, Sequence: seq, Message: "字幕时间范围无效"})
		}
		if strings.TrimSpace(c.Text) == "" {
			issues = append(issues, PreflightIssue{Code: "TEXT_REQUIRED", CueID: id, Sequence: seq, Message: "字幕正文不能为空"})
		}
		if c.EndMs > c.StartMs {
			cps := float64(len([]rune(c.Text))) / (float64(c.EndMs-c.StartMs) / 1000)
			if cps > 25 {
				issues = append(issues, PreflightIssue{Code: "READING_SPEED", CueID: id, Sequence: seq, Message: "阅读速度超过每秒25字"})
			}
		}
		if i > 0 && c.StartMs < j.Cues[ids[i-1]].EndMs {
			issues = append(issues, PreflightIssue{Code: "OVERLAP", CueID: id, Sequence: seq, Message: "字幕段与前一段重叠"})
		}
	}
	return issues
}

type CueEdit struct {
	Op     string `json:"op"`
	Action string `json:"action,omitempty"`
	Type   string `json:"type,omitempty"`
	CueID  string `json:"cueID"`
	Cue    Cue    `json:"cue"`
}
type CueEditResult struct {
	Op            string  `json:"op"`
	CueID         string  `json:"cueID"`
	Sequence      int     `json:"sequence"`
	ReadingSpeed  float64 `json:"readingSpeed"`
	ContentDigest string  `json:"contentDigest"`
}

func (j *ReviewJob) ApplyCueEdits(ops []CueEdit, expected int64) ([]CueEditResult, error) {
	if IsImmutable(j.Status) {
		return nil, ErrImmutableConflict
	}
	if j.Version != expected {
		return nil, ErrConflict
	}
	if !IsEditable(j.Status) {
		return nil, ErrInvalidState
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("%w:编辑批次不能为空", ErrValidation)
	}
	if len(ops) > MaxCueBatchOperations {
		return nil, fmt.Errorf("%w:编辑批次超过100项", ErrValidation)
	}
	copyJob := CloneJob(j)
	seen := map[string]bool{}
	for i, op := range ops {
		if op.Op == "" {
			op.Op = op.Action
		}
		if op.Op == "" {
			op.Op = op.Type
		}
		ops[i].Op = op.Op
		id := op.CueID
		if id == "" {
			id = op.Cue.ID
		}
		if id == "" {
			return nil, fmt.Errorf("%w:操作%d缺少字幕段标识", ErrValidation, i)
		}
		if seen[id] {
			return nil, fmt.Errorf("%w:操作%d重复字幕段", ErrValidation, i)
		}
		seen[id] = true
		switch op.Op {
		case "add":
			if _, ok := copyJob.Cues[id]; ok {
				return nil, fmt.Errorf("%w:操作%d字幕段已存在", ErrValidation, i)
			}
			c := op.Cue
			c.ID = id
			c.JobID = j.ID
			copyJob.Cues[id] = c
		case "update":
			if _, ok := copyJob.Cues[id]; !ok {
				return nil, fmt.Errorf("%w:操作%d字幕段不存在", ErrValidation, i)
			}
			c := op.Cue
			c.ID = id
			c.JobID = j.ID
			copyJob.Cues[id] = c
		case "delete":
			if _, ok := copyJob.Cues[id]; !ok {
				return nil, fmt.Errorf("%w:操作%d字幕段不存在", ErrValidation, i)
			}
			delete(copyJob.Cues, id)
		default:
			return nil, fmt.Errorf("%w:操作%d类型无效", ErrValidation, i)
		}
	}
	issues := copyJob.cueQualityIssues()
	if len(copyJob.Cues) == 0 {
		return nil, fmt.Errorf("%w:字幕段不能为空", ErrValidation)
	}
	if len(issues) > 0 {
		return nil, fmt.Errorf("%w:批次校验失败:%s", ErrValidation, issues[0].Message)
	}
	copyJob.Version++
	results := make([]CueEditResult, 0, len(ops))
	ids := make([]string, 0, len(copyJob.Cues))
	for id := range copyJob.Cues {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, k int) bool {
		a, b := copyJob.Cues[ids[i]], copyJob.Cues[ids[k]]
		if a.StartMs == b.StartMs {
			return ids[i] < ids[k]
		}
		return a.StartMs < b.StartMs
	})
	for seq, id := range ids {
		c := copyJob.Cues[id]
		cps := float64(len([]rune(c.Text))) / (float64(c.EndMs-c.StartMs) / 1000)
		c.Sequence = seq + 1
		c.CharactersPerSecond = cps
		c.ContentHash = Hash(struct {
			S, E  int64
			Sp, T string
		}{c.StartMs, c.EndMs, c.Speaker, c.Text})
		copyJob.Cues[id] = c
	}
	for _, op := range ops {
		id := op.CueID
		if id == "" {
			id = op.Cue.ID
		}
		if c, ok := copyJob.Cues[id]; ok {
			results = append(results, CueEditResult{Op: op.Op, CueID: id, Sequence: c.Sequence, ReadingSpeed: c.CharactersPerSecond, ContentDigest: c.ContentHash})
		}
	}
	*j = *copyJob
	return results, nil
}

type FindingResponse struct {
	FindingID        string `json:"findingID"`
	Note             string `json:"note"`
	ResponseRevision int    `json:"responseRevision"`
	Revision         int    `json:"revision,omitempty"`
	CueContentHash   string `json:"cueContentHash,omitempty"`
	WorkVersion      int64  `json:"workVersion,omitempty"`
}
type ClosureItem struct {
	FindingID string `json:"findingID"`
	Severity  string `json:"severity"`
	CueID     string `json:"cueID"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}
type ClosureReport struct {
	CanResubmit bool          `json:"canResubmit"`
	Version     int64         `json:"version"`
	Revision    int           `json:"revision"`
	Items       []ClosureItem `json:"items"`
}

func (j *ReviewJob) ClosurePreflight(expected int64) ClosureReport {
	r := ClosureReport{Version: j.Version, Revision: j.CurrentRevision + 1}
	if expected != j.Version {
		r.Items = append(r.Items, ClosureItem{Code: "VERSION_MISMATCH", Message: "版本已变化，请重新获取任务"})
		return r
	}
	ids := make([]string, 0, len(j.Findings))
	for id := range j.Findings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		f := j.Findings[id]
		if f.Status != "resolved" {
			r.Items = append(r.Items, ClosureItem{FindingID: id, Severity: f.Severity, CueID: f.CueID, Code: "UNANSWERED", Message: "问题尚未回应"})
			continue
		}
		if f.ResponseRevision != j.CurrentRevision+1 {
			r.Items = append(r.Items, ClosureItem{FindingID: id, Severity: f.Severity, CueID: f.CueID, Code: "REVISION_MISMATCH", Message: "回应修订号不匹配"})
		}
		if _, ok := j.Cues[f.CueID]; !ok {
			r.Items = append(r.Items, ClosureItem{FindingID: id, Severity: f.Severity, CueID: f.CueID, Code: "CUE_MISSING", Message: "关联字幕段已不存在"})
		} else if f.ResponseCueHash != "" && j.Cues[f.CueID].ContentHash != f.ResponseCueHash {
			r.Items = append(r.Items, ClosureItem{FindingID: id, Severity: f.Severity, CueID: f.CueID, Code: "RESPONSE_HASH_MISMATCH", Message: "回应字幕摘要不匹配"})
		}
		if f.ResponseCueHash != "" {
			if snap, ok := j.Snapshots[f.CandidateRevision]; ok {
				if old, ok := snap.Cues[f.CueID]; ok && j.Cues[f.CueID].ContentHash == old.ContentHash {
					r.Items = append(r.Items, ClosureItem{FindingID: id, Severity: f.Severity, CueID: f.CueID, Code: "RESPONSE_UNCHANGED", Message: "回应未产生字幕变化"})
				}
			}
		}
		if f.Status == "resolved" && len(j.ReworkEdits) > 0 {
			proved := false
			for _, proof := range j.ReworkEdits {
				if proof.WorkVersion != f.ResponseWorkVersion || proof.BaseRevision != f.CandidateRevision {
					continue
				}
				for _, cueID := range proof.ChangedCueIDs {
					if cueID == f.CueID {
						proved = true
						break
					}
				}
			}
			if !proved {
				r.Items = append(r.Items, ClosureItem{FindingID: id, Severity: f.Severity, CueID: f.CueID, Code: "REWORK_PROOF_MISSING", Message: "问题回应缺少退修基线证明"})
			}
		}
	}
	r.CanResubmit = true
	for _, it := range r.Items {
		if it.Severity == "blocking" || it.Severity == "high" || it.Code == "VERSION_MISMATCH" {
			r.CanResubmit = false
		}
	}
	return r
}

func (j *ReviewJob) ApplyFindingResponses(items []FindingResponse, expected int64, now time.Time) error {
	if j.Version != expected {
		return ErrConflict
	}
	if j.Status != StatusRework {
		return ErrInvalidState
	}
	if len(items) == 0 {
		return fmt.Errorf("%w:回应批次不能为空", ErrValidation)
	}
	copyJob := CloneJob(j)
	seen := map[string]bool{}
	for _, it := range items {
		if it.ResponseRevision == 0 {
			it.ResponseRevision = it.Revision
		}
		if seen[it.FindingID] {
			return fmt.Errorf("%w:重复问题", ErrValidation)
		}
		seen[it.FindingID] = true
		f, ok := copyJob.Findings[it.FindingID]
		if !ok {
			return ErrNotFound
		}
		if f.Status == "resolved" {
			return fmt.Errorf("%w:问题已回应", ErrValidation)
		}
		if strings.TrimSpace(it.Note) == "" {
			return fmt.Errorf("%w:回应不能为空", ErrValidation)
		}
		if it.ResponseRevision <= 0 || it.ResponseRevision != copyJob.CurrentRevision+1 {
			return fmt.Errorf("%w:回应修订号不匹配", ErrValidation)
		}
		if _, ok := copyJob.Snapshots[it.ResponseRevision]; ok {
			return fmt.Errorf("%w:回应修订不可编辑", ErrValidation)
		}
		if it.CueContentHash != "" {
			cue, exists := copyJob.Cues[f.CueID]
			if !exists || cue.ContentHash != it.CueContentHash {
				return fmt.Errorf("%w:回应字幕摘要不匹配", ErrValidation)
			}
			if snap, exists := copyJob.Snapshots[f.CandidateRevision]; exists {
				if old, ok := snap.Cues[f.CueID]; ok && old.ContentHash == cue.ContentHash {
					return fmt.Errorf("%w:回应未产生字幕变化", ErrValidation)
				}
			}
			f.ResponseCueHash = it.CueContentHash
		}
		if len(copyJob.ReworkEdits) > 0 {
			proof := copyJob.ReworkEdits[len(copyJob.ReworkEdits)-1]
			linked := false
			for _, cueID := range proof.ChangedCueIDs {
				if cueID == f.CueID {
					linked = true
					break
				}
			}
			if proof.WorkVersion != copyJob.Version || !linked {
				return fmt.Errorf("%w:回应未关联当前字幕工作版本", ErrValidation)
			}
		}
		f.Status = "resolved"
		f.ResponseRevision = it.ResponseRevision
		if it.WorkVersion != 0 && it.WorkVersion != copyJob.Version {
			return fmt.Errorf("%w:回应工作版本不匹配", ErrValidation)
		}
		f.ResponseWorkVersion = copyJob.Version
		f.ResponseNote = it.Note
		f.ResolvedAt = &now
		copyJob.Findings[it.FindingID] = f
	}
	copyJob.Version++
	*j = *copyJob
	return nil
}

type ReadinessCheck struct {
	Code    string `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
type ApprovalReadinessReport struct {
	Ready                   bool             `json:"ready"`
	Version                 int64            `json:"version"`
	ChecklistVersion        int              `json:"checklistVersion"`
	ChecklistDigest         string           `json:"checklistDigest"`
	CandidateRevision       int              `json:"candidateRevision"`
	CandidateDigest         string           `json:"candidateDigest"`
	ConclusionSummaryDigest string           `json:"conclusionSummaryDigest"`
	Checks                  []ReadinessCheck `json:"checks"`
	OpenBlocking            []string         `json:"openBlocking"`
}

func (j *ReviewJob) ApprovalReadiness(expected int64, revision int, actor string, candidateDigest ...string) ApprovalReadinessReport {
	r := ApprovalReadinessReport{Version: j.Version, ChecklistVersion: 1, CandidateRevision: revision}
	add := func(code, status, msg string) {
		r.Checks = append(r.Checks, ReadinessCheck{Code: code, Status: status, Message: msg})
	}
	if expected != j.Version {
		add("VERSION_MISMATCH", "blocking", "版本已变化，请重新获取任务")
	}
	if j.Status != StatusPendingApproval {
		add("STATUS_NOT_READY", "blocking", "任务尚未完成审校")
	}
	if len(j.ReviewConclusions) == 0 || j.ReviewConclusions[len(j.ReviewConclusions)-1].ReviewRound != j.ReviewRound {
		add("CONCLUSION_MISSING", "blocking", "当前轮次缺少审校结论")
	}
	if actor == j.CreatedBy || actor == j.SubmittedBy || actor == j.ReviewFinishedBy {
		add("DUTY_SEPARATION", "blocking", "操作者不满足职责分离")
	}
	if j.BlockingOpen() > 0 {
		ids := []string{}
		for id, f := range j.Findings {
			if (f.Severity == "blocking" || f.Severity == "high") && f.Status != "resolved" {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		r.OpenBlocking = ids
		add("OPEN_BLOCKING", "blocking", "存在未闭环阻断问题")
	}
	if snap, ok := j.Snapshots[revision]; !ok {
		add("REVISION_MISSING", "blocking", "候选修订未冻结")
	} else {
		r.CandidateDigest = snap.ContentDigest
		if len(candidateDigest) > 0 && candidateDigest[0] != "" && snap.ContentDigest != candidateDigest[0] {
			add("CANDIDATE_DIGEST_MISMATCH", "blocking", "候选内容摘要不匹配")
		}
	}
	if len(j.ReviewConclusions) > 0 && j.ReviewConclusions[len(j.ReviewConclusions)-1].ReviewRound == j.ReviewRound {
		r.ConclusionSummaryDigest = j.ReviewConclusions[len(j.ReviewConclusions)-1].FindingSummaryDigest
	}
	if len(r.Checks) == 0 {
		add("READY", "pass", "满足批准条件")
	}
	r.Ready = len(r.OpenBlocking) == 0
	for _, c := range r.Checks {
		if c.Status == "blocking" {
			r.Ready = false
		}
	}
	r.ChecklistDigest = Hash(ApprovalChecklistSnapshot{ChecklistVersion: r.ChecklistVersion, CandidateRevision: r.CandidateRevision, CandidateDigest: r.CandidateDigest, ConclusionSummaryDigest: r.ConclusionSummaryDigest, OpenBlocking: len(r.OpenBlocking), DutySeparation: actor != j.CreatedBy && actor != j.SubmittedBy && actor != j.ReviewFinishedBy, Checks: r.Checks})
	return r
}

type CueDiff struct {
	CueID          string   `json:"cueID"`
	Kind           string   `json:"kind"`
	Before         *Cue     `json:"before,omitempty"`
	After          *Cue     `json:"after,omitempty"`
	LinkedFindings []string `json:"linkedFindings,omitempty"`
	Anomaly        string   `json:"anomaly,omitempty"`
}

func (j *ReviewJob) Diff(from, to int) ([]CueDiff, error) {
	a, ok := j.Snapshots[from]
	if !ok {
		return nil, fmt.Errorf("%w:起始修订未冻结", ErrValidation)
	}
	b, ok := j.Snapshots[to]
	if !ok {
		return nil, fmt.Errorf("%w:目标修订未冻结", ErrValidation)
	}
	if from >= to {
		return nil, fmt.Errorf("%w:修订区间无效", ErrValidation)
	}
	ids := map[string]bool{}
	for id := range a.Cues {
		ids[id] = true
	}
	for id := range b.Cues {
		ids[id] = true
	}
	out := []CueDiff{}
	for id := range ids {
		x, xok := a.Cues[id]
		y, yok := b.Cues[id]
		d := CueDiff{CueID: id}
		switch {
		case !xok:
			d.Kind = "added"
			d.After = &y
		case !yok:
			d.Kind = "deleted"
			d.Before = &x
		case x.Text != y.Text:
			d.Kind = "text_changed"
			d.Before = &x
			d.After = &y
		case x.Speaker != y.Speaker:
			d.Kind = "speaker_changed"
			d.Before = &x
			d.After = &y
		case x.StartMs != y.StartMs || x.EndMs != y.EndMs:
			d.Kind = "timeline_changed"
			d.Before = &x
			d.After = &y
		default:
			continue
		}
		for fid, f := range j.Findings {
			if f.ResponseRevision == to && f.CueID == id {
				d.LinkedFindings = append(d.LinkedFindings, fid)
			}
		}
		if len(d.LinkedFindings) == 0 {
			d.Anomaly = "unlinked_change"
		}
		sort.Strings(d.LinkedFindings)
		out = append(out, d)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].CueID < out[k].CueID })
	return out, nil
}

type FindingRoundSummary struct {
	ReviewRound         int    `json:"reviewRound"`
	Category            string `json:"category"`
	Severity            string `json:"severity"`
	Total               int    `json:"total"`
	New                 int    `json:"new"`
	Resolved            int    `json:"resolved"`
	Unresolved          int    `json:"unresolved"`
	AverageResolutionMs int64  `json:"averageResolutionMs"`
	MaxResolutionMs     int64  `json:"maxResolutionMs"`
	WaitingMs           int64  `json:"waitingMs"`
	AverageWaitingMs    int64  `json:"averageWaitingMs"`
}

func findingSeverityRank(s string) int {
	switch s {
	case "blocking":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	default:
		return 3
	}
}

func (j *ReviewJob) FindingSummary(round int, now time.Time) []FindingRoundSummary {
	type summaryKey struct {
		round              int
		category, severity string
	}
	groups := map[summaryKey]*FindingRoundSummary{}
	for _, f := range j.Findings {
		if round > 0 && f.ReviewRound != round {
			continue
		}
		key := summaryKey{f.ReviewRound, f.Category, f.Severity}
		x := groups[key]
		if x == nil {
			x = &FindingRoundSummary{ReviewRound: f.ReviewRound, Category: f.Category, Severity: f.Severity}
			groups[key] = x
		}
		x.Total++
		if f.PreviousFindingID == "" {
			x.New++
		}
		if f.Status == "resolved" {
			x.Resolved++
			if f.ResolvedAt != nil {
				d := f.ResolvedAt.Sub(f.ReportedAt).Milliseconds()
				x.AverageResolutionMs += d
				if d > x.MaxResolutionMs {
					x.MaxResolutionMs = d
				}
			}
		} else {
			x.Unresolved++
			d := now.Sub(f.ReportedAt).Milliseconds()
			if d < 0 {
				d = 0
			}
			x.AverageWaitingMs += d
			if d := now.Sub(f.ReportedAt).Milliseconds(); d > x.WaitingMs {
				x.WaitingMs = d
			}
		}
	}
	out := make([]FindingRoundSummary, 0, len(groups))
	for _, x := range groups {
		if x.Resolved > 0 {
			x.AverageResolutionMs /= int64(x.Resolved)
		}
		if x.Unresolved > 0 {
			x.AverageWaitingMs /= int64(x.Unresolved)
		}
		out = append(out, *x)
	}
	sort.Slice(out, func(i, k int) bool {
		if out[i].ReviewRound != out[k].ReviewRound {
			return out[i].ReviewRound < out[k].ReviewRound
		}
		if out[i].Category != out[k].Category {
			return out[i].Category < out[k].Category
		}
		return findingSeverityRank(out[i].Severity) < findingSeverityRank(out[k].Severity)
	})
	return out
}
