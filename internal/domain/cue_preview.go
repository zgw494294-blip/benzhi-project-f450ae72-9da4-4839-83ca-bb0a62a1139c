package domain

import (
	"sort"
	"strings"
)

const MaxCueBatchOperations = 100

type CueImportIssue struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	OperationIndex int    `json:"operationIndex"`
	CueID          string `json:"cueID,omitempty"`
}

type CueImportItem struct {
	OperationIndex int              `json:"operationIndex"`
	Op             string           `json:"op"`
	CueID          string           `json:"cueID,omitempty"`
	Importable     bool             `json:"importable"`
	ReadingSpeed   float64          `json:"readingSpeed,omitempty"`
	Issues         []CueImportIssue `json:"issues"`
}

type CueCandidateSummary struct {
	CueCount      int    `json:"cueCount"`
	ContentDigest string `json:"contentDigest"`
	DurationMs    int64  `json:"durationMs"`
	LastCueEndMs  int64  `json:"lastCueEndMs"`
	GapIssueCount int    `json:"gapIssueCount"`
}

type CueImportPreview struct {
	Importable      bool                `json:"importable"`
	CurrentVersion  int64               `json:"currentVersion"`
	ExpectedVersion int64               `json:"expectedVersion"`
	Items           []CueImportItem     `json:"items"`
	Issues          []CueImportIssue    `json:"issues"`
	AffectedCueIDs  []string            `json:"affectedCueIDs"`
	Candidate       CueCandidateSummary `json:"candidate"`
}

func (j *ReviewJob) PreviewCueEdits(ops []CueEdit, expected int64) CueImportPreview {
	report := CueImportPreview{CurrentVersion: j.Version, ExpectedVersion: j.Version + 1, Items: make([]CueImportItem, len(ops))}
	copyJob := CloneJob(j)
	add := func(index int, code, id, message string) {
		issue := CueImportIssue{Code: code, Message: message, OperationIndex: index, CueID: id}
		report.Issues = append(report.Issues, issue)
		if index >= 0 && index < len(report.Items) {
			report.Items[index].Issues = append(report.Items[index].Issues, issue)
		}
	}
	if expected != j.Version {
		add(-1, "VERSION_MISMATCH", "", "版本已变化，请重新获取任务")
	}
	if !IsEditable(j.Status) {
		add(-1, "STATUS_NOT_EDITABLE", "", "当前状态不允许编辑字幕")
	}
	if len(ops) == 0 {
		add(-1, "EMPTY_BATCH", "", "编辑批次不能为空")
	}
	if len(ops) > MaxCueBatchOperations {
		add(-1, "BATCH_LIMIT_EXCEEDED", "", "编辑批次超过100项")
	}
	seen := map[string]bool{}
	indexes := map[string]int{}
	for i, original := range ops {
		op := original
		if op.Op == "" {
			op.Op = op.Action
		}
		if op.Op == "" {
			op.Op = op.Type
		}
		id := op.CueID
		if id == "" {
			id = op.Cue.ID
		}
		report.Items[i] = CueImportItem{OperationIndex: i, Op: op.Op, CueID: id, Issues: []CueImportIssue{}}
		if id == "" {
			add(i, "CUE_ID_REQUIRED", "", "字幕段标识不能为空")
			continue
		}
		if seen[id] {
			add(i, "DUPLICATE_CUE_ID", id, "批次内字幕段标识重复")
			continue
		}
		seen[id], indexes[id] = true, i
		switch op.Op {
		case "add":
			if _, exists := copyJob.Cues[id]; exists {
				add(i, "CUE_ALREADY_EXISTS", id, "字幕段已存在")
				continue
			}
			cue := op.Cue
			cue.ID, cue.JobID = id, j.ID
			copyJob.Cues[id] = cue
		case "update":
			if _, exists := copyJob.Cues[id]; !exists {
				add(i, "CUE_NOT_FOUND", id, "字幕段不存在")
				continue
			}
			cue := op.Cue
			cue.ID, cue.JobID = id, j.ID
			copyJob.Cues[id] = cue
		case "delete":
			if _, exists := copyJob.Cues[id]; !exists {
				add(i, "CUE_NOT_FOUND", id, "字幕段不存在")
				continue
			}
			delete(copyJob.Cues, id)
		default:
			add(i, "INVALID_OPERATION", id, "操作类型无效")
		}
	}
	if len(copyJob.Cues) == 0 {
		add(-1, "EMPTY_CANDIDATE", "", "候选字幕不能为空")
	}
	for _, issue := range copyJob.cueQualityIssues() {
		index := -1
		if n, ok := indexes[issue.CueID]; ok {
			index = n
		}
		add(index, issue.Code, issue.CueID, issue.Message)
	}
	gaps := copyJob.coverageIssues(nil)
	for _, issue := range gaps {
		index := -1
		if n, ok := indexes[issue.CueID]; ok {
			index = n
		}
		add(index, issue.Code, issue.CueID, issue.Message)
	}
	for id, index := range indexes {
		if cue, ok := copyJob.Cues[id]; ok && cue.EndMs > cue.StartMs {
			report.Items[index].ReadingSpeed = float64(len([]rune(cue.Text))) / (float64(cue.EndMs-cue.StartMs) / 1000)
		}
	}
	for i := range report.Items {
		report.Items[i].Importable = len(report.Items[i].Issues) == 0
	}
	for id := range indexes {
		report.AffectedCueIDs = append(report.AffectedCueIDs, id)
	}
	sort.Strings(report.AffectedCueIDs)
	last := int64(0)
	for _, cue := range copyJob.Cues {
		if cue.EndMs > last {
			last = cue.EndMs
		}
	}
	report.Candidate = CueCandidateSummary{CueCount: len(copyJob.Cues), ContentDigest: copyJob.ContentDigest(), DurationMs: copyJob.DurationMs, LastCueEndMs: last}
	for _, issue := range gaps {
		if strings.Contains(issue.Code, "GAP") {
			report.Candidate.GapIssueCount++
		}
	}
	sort.SliceStable(report.Issues, func(i, k int) bool {
		if report.Issues[i].OperationIndex == report.Issues[k].OperationIndex {
			if report.Issues[i].CueID == report.Issues[k].CueID {
				return report.Issues[i].Code < report.Issues[k].Code
			}
			return report.Issues[i].CueID < report.Issues[k].CueID
		}
		return report.Issues[i].OperationIndex < report.Issues[k].OperationIndex
	})
	report.Importable = len(report.Issues) == 0
	return report
}
