package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Status string

const (
	StatusDraft           Status = "draft"
	StatusInReview        Status = "in_review"
	StatusRework          Status = "rework"
	StatusPendingApproval Status = "pending_approval"
	StatusApproved        Status = "approved"
	StatusCredentialed    Status = "credentialed"
)

type Cue struct {
	ID                  string  `json:"id"`
	JobID               string  `json:"jobID"`
	Revision            int     `json:"revision"`
	Sequence            int     `json:"sequence"`
	StartMs             int64   `json:"startMs"`
	EndMs               int64   `json:"endMs"`
	Speaker             string  `json:"speaker"`
	Text                string  `json:"text"`
	CharactersPerSecond float64 `json:"charactersPerSecond"`
	ContentHash         string  `json:"contentHash"`
}
type Finding struct {
	ID                  string     `json:"id"`
	JobID               string     `json:"jobID"`
	ReviewRound         int        `json:"reviewRound"`
	CandidateRevision   int        `json:"candidateRevision"`
	CueID               string     `json:"cueID"`
	Category            string     `json:"category"`
	Severity            string     `json:"severity"`
	Evidence            string     `json:"evidence"`
	Status              string     `json:"status"`
	ResponseRevision    int        `json:"responseRevision,omitempty"`
	ResponseWorkVersion int64      `json:"responseWorkVersion,omitempty"`
	ResponseNote        string     `json:"responseNote,omitempty"`
	ReportedBy          string     `json:"reportedBy"`
	ReportedAt          time.Time  `json:"reportedAt"`
	ResolvedAt          *time.Time `json:"resolvedAt,omitempty"`
	Fingerprint         string     `json:"fingerprint"`
	PreviousFindingID   string     `json:"previousFindingID,omitempty"`
	ResponseCueHash     string     `json:"responseCueHash,omitempty"`
}
type ReviewJob struct {
	ID                        string                     `json:"id"`
	ProgramTitle              string                     `json:"programTitle"`
	DurationMs                int64                      `json:"durationMs"`
	Language                  string                     `json:"language"`
	DeliveryBatch             string                     `json:"deliveryBatch"`
	RuleSet                   string                     `json:"ruleSet"`
	Status                    Status                     `json:"status"`
	CurrentRevision           int                        `json:"currentRevision"`
	ReviewRound               int                        `json:"reviewRound"`
	Version                   int64                      `json:"version"`
	CreatedBy                 string                     `json:"createdBy"`
	CreatedAt                 time.Time                  `json:"createdAt"`
	ApprovedBy                string                     `json:"approvedBy,omitempty"`
	ApprovedAt                *time.Time                 `json:"approvedAt,omitempty"`
	SubmittedBy               string                     `json:"submittedBy,omitempty"`
	ReviewFinishedBy          string                     `json:"reviewFinishedBy,omitempty"`
	ApprovalRevision          int                        `json:"approvalRevision,omitempty"`
	ApprovalDigest            string                     `json:"approvalDigest,omitempty"`
	ApprovalNote              string                     `json:"approvalNote,omitempty"`
	ApprovalChecklistDigest   string                     `json:"approvalChecklistDigest,omitempty"`
	ApprovalChecklistSnapshot *ApprovalChecklistSnapshot `json:"approvalChecklist,omitempty"`
	Cues                      map[string]Cue             `json:"cues"`
	Findings                  map[string]Finding         `json:"findings"`
	Snapshots                 map[int]CandidateSnapshot  `json:"snapshots"`
	ReviewConclusions         []ReviewConclusion         `json:"reviewConclusions,omitempty"`
	ReworkEdits               []ReworkEditProof          `json:"reworkEdits,omitempty"`
	Events                    []Event                    `json:"events,omitempty"`
}
type CandidateSnapshot struct {
	Revision      int            `json:"revision"`
	ReviewRound   int            `json:"reviewRound"`
	SubmittedBy   string         `json:"submittedBy"`
	SubmittedAt   time.Time      `json:"submittedAt"`
	ContentDigest string         `json:"contentDigest"`
	Cues          map[string]Cue `json:"cues"`
}
type MetadataRevision struct {
	ProgramTitle  string `json:"programTitle"`
	DurationMs    int64  `json:"durationMs"`
	Language      string `json:"language"`
	DeliveryBatch string `json:"deliveryBatch"`
	RuleSet       string `json:"ruleSet"`
}
type SeverityCounts struct {
	Blocking int `json:"blocking"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
}
type FindingStatistics struct {
	ReviewRound  int            `json:"reviewRound"`
	BySeverity   SeverityCounts `json:"bySeverity"`
	OpenBlocking int            `json:"openBlocking"`
	Total        int            `json:"total"`
}
type ReviewConclusion struct {
	ReviewRound          int               `json:"reviewRound"`
	Reviewer             string            `json:"reviewer"`
	ConclusionNote       string            `json:"conclusionNote"`
	FindingSummary       FindingStatistics `json:"findingSummary"`
	FindingSummaryDigest string            `json:"findingSummaryDigest"`
	FinishedAt           time.Time         `json:"finishedAt"`
	ResultStatus         Status            `json:"resultStatus"`
}
type ReworkEditProof struct {
	BaseRevision  int       `json:"baseRevision"`
	BaseDigest    string    `json:"baseDigest"`
	ChangedCueIDs []string  `json:"changedCueIDs"`
	WorkVersion   int64     `json:"workVersion"`
	Actor         string    `json:"actor"`
	EditedAt      time.Time `json:"editedAt"`
}
type ApprovalChecklistSnapshot struct {
	ChecklistVersion        int              `json:"checklistVersion"`
	CandidateRevision       int              `json:"candidateRevision"`
	CandidateDigest         string           `json:"candidateDigest"`
	ConclusionSummaryDigest string           `json:"conclusionSummaryDigest"`
	OpenBlocking            int              `json:"openBlocking"`
	DutySeparation          bool             `json:"dutySeparation"`
	Checks                  []ReadinessCheck `json:"checks"`
}
type Credential struct {
	ID               string    `json:"id"`
	JobID            string    `json:"jobID"`
	Revision         int       `json:"revision"`
	ContentDigest    string    `json:"contentDigest"`
	EventChainDigest string    `json:"eventChainDigest"`
	ApprovedBy       string    `json:"approvedBy"`
	IssuedBy         string    `json:"issuedBy"`
	IssuedAt         time.Time `json:"issuedAt"`
	CredentialDigest string    `json:"credentialDigest"`
	Algorithm        string    `json:"algorithm"`
}
type Event struct {
	Sequence   int64           `json:"sequence"`
	Type       string          `json:"type"`
	JobID      string          `json:"jobID"`
	Version    int64           `json:"version"`
	At         time.Time       `json:"at"`
	Actor      string          `json:"actor"`
	Data       json.RawMessage `json:"data"`
	PrevDigest string          `json:"prevDigest"`
	Digest     string          `json:"digest"`
}

var (
	ErrNotFound            = errors.New("任务不存在")
	ErrConflict            = errors.New("版本冲突")
	ErrInvalidState        = errors.New("当前状态不允许该操作")
	ErrValidation          = errors.New("校验失败")
	ErrForbidden           = errors.New("职责分离校验失败")
	ErrIdempotencyConflict = errors.New("幂等键与请求参数冲突")
	ErrImmutableConflict   = errors.New("不可变资源冲突")
	ErrIntegrity           = errors.New("事件链完整性错误")
	ErrSnapshotNotFrozen   = errors.New("候选修订未冻结")
	ErrCandidateDigest     = errors.New("候选摘要冲突")
	ErrBaselineConflict    = errors.New("退修基线冲突")
	ErrChecklistDigest     = errors.New("批准清单摘要冲突")
)

func EventRecordDigest(e Event) string {
	return Hash(struct {
		Sequence    int64
		Type, JobID string
		Version     int64
		Data        json.RawMessage
		Prev        string
	}{e.Sequence, e.Type, e.JobID, e.Version, e.Data, e.PrevDigest})
}

func (j *ReviewJob) ValidateCues() error {
	if len(j.Cues) == 0 {
		return fmt.Errorf("%w:字幕段不能为空", ErrValidation)
	}
	cues := make([]Cue, 0, len(j.Cues))
	for _, c := range j.Cues {
		if c.StartMs < 0 || c.EndMs <= c.StartMs || (j.DurationMs > 0 && c.EndMs > j.DurationMs) {
			return fmt.Errorf("%w:时间范围无效", ErrValidation)
		}
		if strings.TrimSpace(c.Text) == "" {
			return fmt.Errorf("%w:正文不能为空", ErrValidation)
		}
		dur := float64(c.EndMs-c.StartMs) / 1000
		cps := float64(len([]rune(c.Text))) / dur
		if cps > 25 {
			return fmt.Errorf("%w:阅读速度过快", ErrValidation)
		}
		c.CharactersPerSecond = cps
		cues = append(cues, c)
	}
	sort.Slice(cues, func(i, k int) bool { return cues[i].StartMs < cues[k].StartMs })
	for i := 1; i < len(cues); i++ {
		if cues[i].StartMs < cues[i-1].EndMs {
			return fmt.Errorf("%w:字幕段重叠", ErrValidation)
		}
	}
	return nil
}
func (j *ReviewJob) ContentDigest() string {
	ids := make([]string, 0, len(j.Cues))
	for id := range j.Cues {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		c := j.Cues[id]
		fmt.Fprintf(h, "%s|%d|%d|%s|%s;", id, c.StartMs, c.EndMs, c.Speaker, c.Text)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func (j *ReviewJob) BlockingOpen() int {
	n := 0
	for _, f := range j.Findings {
		if (f.Severity == "blocking" || f.Severity == "high") && f.Status != "resolved" {
			n++
		}
	}
	return n
}
func Hash(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func FindingFingerprint(cueID, category, severity, evidence string) string {
	return Hash(struct{ CueID, Category, Severity, Evidence string }{cueID, strings.TrimSpace(category), strings.TrimSpace(severity), strings.Join(strings.Fields(evidence), " ")})
}

func CloneJob(j *ReviewJob) *ReviewJob {
	b, _ := json.Marshal(j)
	var out ReviewJob
	_ = json.Unmarshal(b, &out)
	if out.Cues == nil {
		out.Cues = map[string]Cue{}
	}
	if out.Findings == nil {
		out.Findings = map[string]Finding{}
	}
	if out.Snapshots == nil {
		out.Snapshots = map[int]CandidateSnapshot{}
	}
	return &out
}
