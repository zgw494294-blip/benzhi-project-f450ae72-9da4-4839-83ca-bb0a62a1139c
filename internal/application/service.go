package application

import (
	"caption-delivery-qc/internal/domain"
	"caption-delivery-qc/internal/journal"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Service struct {
	store *journal.Store
	mu    sync.Mutex
	now   func() time.Time
	idem  map[string]string
}

func New(store *journal.Store) *Service {
	return &Service{store: store, now: func() time.Time { return time.Now().UTC() }, idem: map[string]string{}}
}
func newID() string { b := make([]byte, 16); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func cloneCueResults(in []domain.CueEditResult) []domain.CueEditResult {
	if in == nil {
		return nil
	}
	out := make([]domain.CueEditResult, len(in))
	copy(out, in)
	return out
}

type CreateJobInput struct {
	ProgramTitle   string `json:"programTitle"`
	DurationMs     int64  `json:"durationMs"`
	Language       string `json:"language"`
	DeliveryBatch  string `json:"deliveryBatch"`
	RuleSet        string `json:"ruleSet"`
	Actor          string `json:"actor"`
	IdempotencyKey string
}

func (s *Service) ReviseMetadata(id string, next domain.MetadataRevision, expected int64, actor, idempotencyKey string) (*domain.ReviewJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	digest := domain.Hash(struct {
		JobID    string
		Next     domain.MetadataRevision
		Expected int64
	}{id, next, expected})
	key := "metadata\x00" + actor + "\x00" + idempotencyKey
	if idempotencyKey != "" {
		if rec, ok := s.store.Idempotency(key); ok {
			if rec.Digest != digest {
				return nil, domain.ErrIdempotencyConflict
			}
			if rec.JobResult != nil {
				return domain.CloneJob(rec.JobResult), nil
			}
			return s.store.Get(id)
		}
	}
	if err = j.ReviseMetadata(next, expected); err != nil {
		return nil, err
	}
	record := journal.IdempotencyRecord{Actor: actor, Digest: digest, JobID: id, Version: j.Version, JobResult: domain.CloneJob(j)}
	if idempotencyKey != "" {
		err = s.store.SaveIdempotent(j, "metadata.revised", actor, next, key, record)
	} else {
		err = s.store.Save(j, "metadata.revised", actor, next)
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

func (s *Service) CreateJob(in CreateJobInput) (*domain.ReviewJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := domain.ValidateMetadata(in.ProgramTitle, in.Language, in.DeliveryBatch, in.RuleSet, in.DurationMs); e != nil {
		return nil, e
	}
	if in.Actor == "" {
		in.Actor = "system"
	}
	digest := domain.Hash(struct {
		Actor, ProgramTitle              string
		DurationMs                       int64
		Language, DeliveryBatch, RuleSet string
	}{in.Actor, in.ProgramTitle, in.DurationMs, in.Language, in.DeliveryBatch, in.RuleSet})
	key := ""
	if in.IdempotencyKey != "" {
		key = "create\x00" + in.IdempotencyKey
		if rec, ok := s.store.Idempotency(key); ok {
			if rec.Digest != digest {
				return nil, domain.ErrIdempotencyConflict
			}
			return s.store.Get(rec.JobID)
		}
	}
	j := domain.NewJob(newID(), in.ProgramTitle, in.Language, in.DeliveryBatch, in.RuleSet, in.Actor, in.DurationMs, s.now())
	if err := s.store.Create(j, key, journal.IdempotencyRecord{Actor: in.Actor, Digest: digest, JobID: j.ID}); err != nil {
		return nil, err
	}
	if in.IdempotencyKey != "" {
		s.idem[in.IdempotencyKey] = j.ID
	}
	return j, nil
}

func (s *Service) Preflight(id string, expected int64, threshold ...int64) (domain.PreflightReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.store.Get(id)
	if e != nil {
		return domain.PreflightReport{}, e
	}
	return j.SubmissionPreflight(expected, threshold...), nil
}
func (s *Service) BatchCues(id string, ops []domain.CueEdit, expected int64, actor string, importKey ...string) (*domain.ReviewJob, []domain.CueEditResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.store.Get(id)
	if e != nil {
		return nil, nil, e
	}
	key := ""
	if len(importKey) > 0 && strings.TrimSpace(importKey[0]) != "" {
		key = "cue-import\x00" + actor + "\x00" + importKey[0]
	}
	digest := domain.Hash(struct {
		JobID      string
		Expected   int64
		Operations []domain.CueEdit
	}{id, expected, ops})
	if key != "" {
		if rec, ok := s.store.Idempotency(key); ok {
			if rec.Digest != digest {
				return nil, nil, domain.ErrIdempotencyConflict
			}
			if rec.JobResult != nil {
				return domain.CloneJob(rec.JobResult), rec.CueResults, nil
			}
			return j, rec.CueResults, nil
		}
	}
	res, e := j.ApplyCueEdits(ops, expected)
	if e != nil {
		return nil, nil, e
	}
	rec := journal.IdempotencyRecord{Actor: actor, Digest: digest, JobID: id, Version: j.Version, CueResults: cloneCueResults(res), JobResult: domain.CloneJob(j)}
	if key != "" {
		e = s.store.SaveIdempotent(j, "cue.batch", actor, ops, key, rec)
	} else {
		e = s.store.Save(j, "cue.batch", actor, ops)
	}
	if e != nil {
		return nil, nil, e
	}
	return j, res, nil
}
func (s *Service) PreviewCues(id string, ops []domain.CueEdit, expected int64) (domain.CueImportPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.VerifyIntegrity(); err != nil {
		return domain.CueImportPreview{}, err
	}
	j, err := s.store.Get(id)
	if err != nil {
		return domain.CueImportPreview{}, err
	}
	return j.PreviewCueEdits(ops, expected), nil
}
func (s *Service) Get(id string) (*domain.ReviewJob, error) { return s.store.Get(id) }
func (s *Service) UpsertCue(id string, c domain.Cue, expected int64, actor string) (*domain.ReviewJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.store.Get(id)
	if e != nil {
		return nil, e
	}
	if c.ID == "" {
		c.ID = newID()
	}
	op := "add"
	if _, ok := j.Cues[c.ID]; ok {
		op = "update"
	}
	res, e := j.ApplyCueEdits([]domain.CueEdit{{Op: op, CueID: c.ID, Cue: c}}, expected)
	_ = res
	if e != nil {
		return nil, e
	}
	if e = s.store.Save(j, "cue.upsert", actor, c); e != nil {
		return nil, e
	}
	return j, nil
}
func (s *Service) DeleteCue(id, cueID string, expected int64, actor string) (*domain.ReviewJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.store.Get(id)
	if e != nil {
		return nil, e
	}
	_, e = j.ApplyCueEdits([]domain.CueEdit{{Op: "delete", CueID: cueID}}, expected)
	if e != nil {
		return nil, e
	}
	e = s.store.Save(j, "cue.delete", actor, map[string]string{"cueID": cueID})
	return j, e
}
func (s *Service) Submit(id string, expected int64, actor string) (*domain.ReviewJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.store.Get(id)
	if e != nil {
		return nil, e
	}
	e = j.FreezeSubmit(expected, actor, s.now())
	if e != nil {
		return nil, e
	}
	e = s.store.Save(j, "review.submitted", actor, j.Snapshots[j.CurrentRevision])
	return j, e
}
func (s *Service) AddFinding(id string, f domain.Finding, expected int64, actor string) (*domain.ReviewJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.store.Get(id)
	if e != nil {
		return nil, e
	}
	if domain.IsImmutable(j.Status) {
		return nil, domain.ErrImmutableConflict
	}
	if j.Version != expected {
		return nil, domain.ErrConflict
	}
	if j.Status != domain.StatusInReview {
		return nil, domain.ErrInvalidState
	}
	if strings.TrimSpace(actor) == "" || actor == "anonymous" || actor == j.CreatedBy || actor == j.SubmittedBy {
		return nil, domain.ErrForbidden
	}
	if f.ID == "" {
		f.ID = newID()
	}
	f.ReportedBy = actor
	f.ReportedAt = s.now()
	e = j.AddFinding(f, expected)
	if e != nil {
		return nil, e
	}
	e = s.store.Save(j, "finding.reported", actor, f)
	return j, e
}
func (s *Service) BatchFindings(id string, fs []domain.Finding, expected int64, actor string) (*domain.ReviewJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.store.Get(id)
	if e != nil {
		return nil, e
	}
	if domain.IsImmutable(j.Status) {
		return nil, domain.ErrImmutableConflict
	}
	if j.Version != expected {
		return nil, domain.ErrConflict
	}
	if j.Status != domain.StatusInReview {
		return nil, domain.ErrInvalidState
	}
	if strings.TrimSpace(actor) == "" || actor == "anonymous" || actor == j.CreatedBy || actor == j.SubmittedBy {
		return nil, domain.ErrForbidden
	}
	if len(fs) == 0 {
		return nil, fmt.Errorf("%w:问题批次不能为空", domain.ErrValidation)
	}
	copyJob := domain.CloneJob(j)
	seen := map[string]bool{}
	saved := make([]domain.Finding, 0, len(fs))
	for _, f := range fs {
		if f.ID == "" {
			f.ID = newID()
		}
		fingerprint := domain.FindingFingerprint(f.CueID, f.Category, f.Severity, f.Evidence)
		if seen[fingerprint] {
			return nil, fmt.Errorf("%w:批次内重复问题", domain.ErrValidation)
		}
		seen[fingerprint] = true
		f.ReportedBy = actor
		f.ReportedAt = s.now()
		if e = copyJob.AddFinding(f, copyJob.Version); e != nil {
			return nil, e
		}
		copyJob.Version = j.Version
		saved = append(saved, copyJob.Findings[f.ID])
	}
	copyJob.Version = j.Version + 1
	*j = *copyJob
	if e = s.store.Save(j, "finding.batch", actor, saved); e != nil {
		return nil, e
	}
	return j, nil
}
func (s *Service) FindFindings(id string, q FindingQuery) (FindingResult, error) {
	j, e := s.store.Get(id)
	if e != nil {
		return FindingResult{}, e
	}
	return FilterFindings(j, q), nil
}
func (s *Service) FinishReview(id string, expected int64, actor string) (*domain.ReviewJob, error) {
	j, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	_, digest := j.FindingStatisticsDigest(j.ReviewRound)
	return s.FinishReviewWithConclusion(id, expected, actor, "兼容流程审校结论", digest)
}
func (s *Service) FinishReviewWithConclusion(id string, expected int64, actor, note, digest string) (*domain.ReviewJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.store.Get(id)
	if e != nil {
		return nil, e
	}
	e = j.FinishReviewWithConclusion(expected, actor, note, digest, s.now())
	if e != nil {
		return nil, e
	}
	e = s.store.Save(j, "review.finished", actor, j.ReviewConclusions[len(j.ReviewConclusions)-1])
	return j, e
}

func (s *Service) Snapshot(id string, revision int, digest string) (CandidateSnapshotView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.VerifyIntegrity(); err != nil {
		return CandidateSnapshotView{}, err
	}
	j, err := s.store.Get(id)
	if err != nil {
		return CandidateSnapshotView{}, err
	}
	snap, err := j.Snapshot(revision, digest)
	if err != nil {
		return CandidateSnapshotView{}, err
	}
	return snapshotView(id, snap), nil
}

func (s *Service) FindingStatistics(id string, round int) (domain.FindingStatistics, string, error) {
	j, err := s.store.Get(id)
	if err != nil {
		return domain.FindingStatistics{}, "", err
	}
	if round <= 0 {
		round = j.ReviewRound
	}
	stats, digest := j.FindingStatisticsDigest(round)
	return stats, digest, nil
}

func (s *Service) ReworkCues(id string, ops []domain.CueEdit, expected int64, baseRevision int, baseDigest, actor string) (*domain.ReviewJob, []domain.CueEditResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.store.Get(id)
	if err != nil {
		return nil, nil, err
	}
	if domain.IsImmutable(j.Status) {
		return nil, nil, domain.ErrImmutableConflict
	}
	if j.Status != domain.StatusRework {
		return nil, nil, domain.ErrInvalidState
	}
	if j.Version != expected {
		return nil, nil, domain.ErrConflict
	}
	if strings.TrimSpace(actor) == "" || actor == "anonymous" || actor == j.ReviewFinishedBy {
		return nil, nil, domain.ErrForbidden
	}
	res, err := j.ApplyReworkCueEdits(ops, expected, baseRevision, baseDigest, actor, s.now())
	if err != nil {
		return nil, nil, err
	}
	if err = s.store.Save(j, "rework.cues.edited", actor, j.ReworkEdits[len(j.ReworkEdits)-1]); err != nil {
		return nil, nil, err
	}
	return j, res, nil
}
func (s *Service) Respond(id, fid, note string, revision int, expected int64, actor string, cueHash ...string) (*domain.ReviewJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.store.Get(id)
	if e != nil {
		return nil, e
	}
	if domain.IsImmutable(j.Status) {
		return nil, domain.ErrImmutableConflict
	}
	if j.Status != domain.StatusRework {
		return nil, domain.ErrInvalidState
	}
	if j.Version != expected {
		return nil, domain.ErrConflict
	}
	if strings.TrimSpace(actor) == "" || actor == "anonymous" || actor == j.ReviewFinishedBy {
		return nil, domain.ErrForbidden
	}
	if len(cueHash) == 0 || strings.TrimSpace(cueHash[0]) == "" {
		return nil, fmt.Errorf("%w:回应必须提供字幕摘要", domain.ErrValidation)
	}
	if revision == 0 {
		revision = j.CurrentRevision + 1
	}
	item := domain.FindingResponse{FindingID: fid, Note: note, ResponseRevision: revision}
	if len(cueHash) > 0 {
		item.CueContentHash = cueHash[0]
	}
	e = j.ApplyFindingResponses([]domain.FindingResponse{item}, expected, s.now())
	if e != nil {
		return nil, e
	}
	e = s.store.Save(j, "finding.responded", actor, item)
	return j, e
}
func (s *Service) BatchRespond(id string, items []domain.FindingResponse, expected int64, actor string) (*domain.ReviewJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.store.Get(id)
	if e != nil {
		return nil, e
	}
	if domain.IsImmutable(j.Status) {
		return nil, domain.ErrImmutableConflict
	}
	if j.Status != domain.StatusRework {
		return nil, domain.ErrInvalidState
	}
	if j.Version != expected {
		return nil, domain.ErrConflict
	}
	if strings.TrimSpace(actor) == "" || actor == "anonymous" || actor == j.ReviewFinishedBy {
		return nil, domain.ErrForbidden
	}
	for _, item := range items {
		if strings.TrimSpace(item.CueContentHash) == "" {
			return nil, fmt.Errorf("%w:回应必须提供字幕摘要", domain.ErrValidation)
		}
	}
	if e = j.ApplyFindingResponses(items, expected, s.now()); e != nil {
		return nil, e
	}
	if e = s.store.Save(j, "finding.response.batch", actor, items); e != nil {
		return nil, e
	}
	return j, nil
}
func (s *Service) ClosurePreflight(id string, expected int64) (domain.ClosureReport, error) {
	j, e := s.store.Get(id)
	if e != nil {
		return domain.ClosureReport{}, e
	}
	return j.ClosurePreflight(expected), nil
}
func (s *Service) Approve(id string, expected int64, actor string, evidence ...any) (*domain.ReviewJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.store.Get(id)
	if e != nil {
		return nil, e
	}
	if len(evidence) >= 4 {
		candidateRevision, _ := evidence[0].(int)
		candidateDigest, _ := evidence[1].(string)
		checklistDigest, _ := evidence[2].(string)
		note, _ := evidence[3].(string)
		e = j.ApproveWithChecklist(expected, actor, candidateRevision, candidateDigest, checklistDigest, note, s.now())
	} else {
		e = j.Approve(expected, actor, s.now(), evidence...)
	}
	if e != nil {
		return nil, e
	}
	e = s.store.Save(j, "job.approved", actor, map[string]any{"candidateRevision": j.ApprovalRevision, "candidateDigest": j.ApprovalDigest, "checklistDigest": j.ApprovalChecklistDigest, "signNote": j.ApprovalNote, "checklist": j.ApprovalChecklistSnapshot})
	return j, e
}
func (s *Service) Readiness(id string, expected int64, revision int, actor string, digest ...string) (domain.ApprovalReadinessReport, error) {
	j, e := s.store.Get(id)
	if e != nil {
		return domain.ApprovalReadinessReport{}, e
	}
	if revision == 0 {
		revision = j.CurrentRevision
	}
	return j.ApprovalReadiness(expected, revision, actor, digest...), nil
}
func (s *Service) Checklist(id string, expected int64, actor string) (domain.ApprovalChecklistSnapshot, string, error) {
	j, err := s.store.Get(id)
	if err != nil {
		return domain.ApprovalChecklistSnapshot{}, "", err
	}
	check, digest := j.ApprovalChecklist(expected, actor)
	return check, digest, nil
}
func (s *Service) Diff(id string, from, to int) ([]domain.CueDiff, error) {
	j, e := s.store.Get(id)
	if e != nil {
		return nil, e
	}
	return j.Diff(from, to)
}
func (s *Service) Credential(id string, expected int64, actor string, idempotency ...string) (domain.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.store.Get(id)
	if e != nil {
		return domain.Credential{}, e
	}
	if len(idempotency) == 0 || idempotency[0] == "" {
		return domain.Credential{}, fmt.Errorf("%w:签发必须提供 Idempotency-Key", domain.ErrValidation)
	}
	key := "credential\x00" + actor + "\x00" + idempotency[0]
	digest := domain.Hash(struct {
		JobID    string
		Revision int
		Actor    string
	}{id, j.CurrentRevision, actor})
	if rec, ok := s.store.Idempotency(key); ok {
		if rec.Digest != digest {
			return domain.Credential{}, domain.ErrIdempotencyConflict
		}
		if existing, found := s.store.Credential(rec.JobID); found {
			return existing, nil
		}
	}
	if _, ok := s.store.Credential(id); ok {
		return domain.Credential{}, domain.ErrImmutableConflict
	}
	c, e := j.Credential(expected, actor, s.now(), s.store.EventDigest(id))
	if e != nil {
		return domain.Credential{}, e
	}
	e = s.store.CommitCredential(j, *c, key, journal.IdempotencyRecord{Actor: actor, Digest: digest, JobID: id}, actor)
	return *c, e
}
func (s *Service) Timeline(id string) ([]domain.Event, error) {
	if _, e := s.store.Get(id); e != nil {
		return nil, e
	}
	return s.store.Events(id), nil
}

type TimelineQuery struct {
	Type, Actor string
	Cursor      int64
	Limit       int
}

func (s *Service) TimelinePage(id string, q TimelineQuery) (journal.EventPage, error) {
	if _, e := s.store.Get(id); e != nil {
		return journal.EventPage{}, e
	}
	return s.store.PageEvents(journal.EventQuery{JobID: id, Type: q.Type, Actor: q.Actor, Cursor: q.Cursor, Limit: q.Limit})
}
func (s *Service) VerifyCredential(id string) (bool, domain.Credential, error) {
	c, ok := s.store.Credential(id)
	if !ok {
		return false, domain.Credential{}, domain.ErrNotFound
	}
	j, e := s.store.Get(id)
	if e != nil {
		return false, c, e
	}
	events := s.store.Events(id)
	if len(events) > 0 && events[len(events)-1].Type == "credential.issued" {
		events = events[:len(events)-1]
	}
	historical := j.Snapshots[c.Revision].ContentDigest
	valid := historical != "" && c.ContentDigest == historical && c.EventChainDigest == domain.Hash(events) && c.CredentialDigest == domain.Hash(&domain.Credential{ID: c.ID, JobID: c.JobID, Revision: c.Revision, ContentDigest: c.ContentDigest, EventChainDigest: c.EventChainDigest, ApprovedBy: c.ApprovedBy, IssuedBy: c.IssuedBy, IssuedAt: c.IssuedAt, Algorithm: c.Algorithm})
	return valid, c, nil
}
func (s *Service) VerifyCredentialCopy(id string, submitted domain.Credential) (map[string]any, error) {
	if submitted.ID == "" || submitted.JobID == "" || submitted.ContentDigest == "" || submitted.EventChainDigest == "" || submitted.CredentialDigest == "" || submitted.Algorithm != "SHA-256" || len(submitted.ContentDigest) != 64 || len(submitted.EventChainDigest) != 64 || len(submitted.CredentialDigest) != 64 {
		return nil, fmt.Errorf("%w:凭据字段或摘要格式无效", domain.ErrValidation)
	}
	if _, e := hex.DecodeString(submitted.ContentDigest); e != nil {
		return nil, fmt.Errorf("%w:内容摘要格式无效", domain.ErrValidation)
	}
	if _, e := hex.DecodeString(submitted.EventChainDigest); e != nil {
		return nil, fmt.Errorf("%w:事件链摘要格式无效", domain.ErrValidation)
	}
	if _, e := hex.DecodeString(submitted.CredentialDigest); e != nil {
		return nil, fmt.Errorf("%w:凭据摘要格式无效", domain.ErrValidation)
	}
	saved, ok := s.store.Credential(id)
	if !ok {
		return nil, domain.ErrNotFound
	}
	if submitted.JobID != id || submitted.ID != saved.ID {
		return map[string]any{"valid": false, "algorithm": submitted.Algorithm, "revision": submitted.Revision, "checks": map[string]any{"credentialIdentity": false}, "diffs": []map[string]string{{"path": "credentialIdentity", "expected": saved.ID, "submitted": submitted.ID}}}, nil
	}
	j, e := s.store.Get(id)
	if e != nil {
		return nil, e
	}
	events := s.store.Events(id)
	if len(events) > 0 && events[len(events)-1].Type == "credential.issued" {
		events = events[:len(events)-1]
	}
	historical := j.Snapshots[submitted.Revision].ContentDigest
	content := historical != "" && submitted.ContentDigest == historical
	currentMatchesHistorical := historical != "" && j.ContentDigest() == historical
	chain := submitted.EventChainDigest == domain.Hash(events)
	computedCredentialDigest := domain.Hash(&domain.Credential{ID: submitted.ID, JobID: submitted.JobID, Revision: submitted.Revision, ContentDigest: submitted.ContentDigest, EventChainDigest: submitted.EventChainDigest, ApprovedBy: submitted.ApprovedBy, IssuedBy: submitted.IssuedBy, IssuedAt: submitted.IssuedAt, Algorithm: submitted.Algorithm})
	// A self-consistent forged copy is still invalid: it must match the
	// immutable credential projection as well as the canonical digest.
	cred := submitted.CredentialDigest == saved.CredentialDigest && submitted.CredentialDigest == computedCredentialDigest
	valid := content && chain && cred
	checks := map[string]any{"contentDigest": content, "eventChainDigest": chain, "credentialDigest": cred, "candidateRevision": historical != ""}
	diffs := []map[string]string{}
	prefix := func(v string) string {
		if len(v) > 12 {
			return v[:12]
		}
		return v
	}
	if !content {
		diffs = append(diffs, map[string]string{"path": "contentDigest", "expected": prefix(historical), "submitted": prefix(submitted.ContentDigest)})
	}
	if !chain {
		diffs = append(diffs, map[string]string{"path": "eventChainDigest", "expected": prefix(domain.Hash(events)), "submitted": prefix(submitted.EventChainDigest)})
	}
	if !cred {
		diffs = append(diffs, map[string]string{"path": "credentialDigest", "expected": prefix(saved.CredentialDigest), "submitted": prefix(submitted.CredentialDigest)})
	}
	return map[string]any{"valid": valid, "algorithm": submitted.Algorithm, "revision": submitted.Revision, "checks": checks, "diffs": diffs, "historicalRevisionMismatch": historical == "" || !currentMatchesHistorical, "savedCredential": saved}, nil
}

func (s *Service) VerifyCredentialsBatch(credentials []domain.Credential) (map[string]any, error) {
	if len(credentials) == 0 || len(credentials) > 50 {
		return nil, fmt.Errorf("%w:凭据数量必须在1到50之间", domain.ErrValidation)
	}
	if err := s.store.VerifyIntegrity(); err != nil {
		return nil, err
	}
	seenJobs, seenCredentials := map[string]bool{}, map[string]bool{}
	results := make([]map[string]any, 0, len(credentials))
	validCount, invalidCount := 0, 0
	failures := map[string]int{}
	for _, c := range credentials {
		if c.JobID == "" || c.ID == "" || seenJobs[c.JobID] || seenCredentials[c.ID] {
			return nil, fmt.Errorf("%w:任务标识重复或为空", domain.ErrValidation)
		}
		seenJobs[c.JobID], seenCredentials[c.ID] = true, true
		result, err := s.VerifyCredentialCopy(c.JobID, c)
		if err != nil {
			if errors.Is(err, domain.ErrIntegrity) {
				return nil, err
			}
			result = map[string]any{"valid": false, "algorithm": c.Algorithm, "revision": c.Revision, "checks": map[string]any{"credentialFormat": false}, "diffs": []map[string]string{{"path": "credential", "submitted": err.Error()}}}
		}
		if ok, _ := result["valid"].(bool); ok {
			validCount++
		} else {
			invalidCount++
			if checks, ok := result["checks"].(map[string]any); ok {
				for name, value := range checks {
					if valid, ok := value.(bool); ok && !valid {
						failures[name]++
					}
				}
			}
		}
		result["jobID"] = c.JobID
		results = append(results, result)
	}
	return map[string]any{"results": results, "validCount": validCount, "invalidCount": invalidCount, "failureCounts": failures}, nil
}

type FindingQuery struct {
	ReviewRound                int
	CandidateRevision          int
	Category, Severity, Status string
	Cursor                     int
	Limit                      int
	Summary                    bool
}
type FindingResult struct {
	Findings     []domain.Finding             `json:"findings"`
	Counts       map[string]int               `json:"counts"`
	OpenBlocking int                          `json:"openBlocking"`
	NextCursor   int                          `json:"nextCursor,omitempty"`
	Summary      []domain.FindingRoundSummary `json:"summary,omitempty"`
}

func FilterFindings(j *domain.ReviewJob, q FindingQuery) FindingResult {
	fs := []domain.Finding{}
	for _, f := range j.Findings {
		if q.ReviewRound > 0 && f.ReviewRound != q.ReviewRound {
			continue
		}
		if q.CandidateRevision > 0 && f.CandidateRevision != q.CandidateRevision {
			continue
		}
		if q.Category != "" && f.Category != q.Category {
			continue
		}
		if q.Severity != "" && f.Severity != q.Severity {
			continue
		}
		if q.Status != "" && f.Status != q.Status {
			continue
		}
		fs = append(fs, f)
	}
	sort.Slice(fs, func(i, k int) bool {
		ci, cj := j.Cues[fs[i].CueID], j.Cues[fs[k].CueID]
		if ci.Sequence != cj.Sequence {
			return ci.Sequence < cj.Sequence
		}
		if fs[i].Severity != fs[k].Severity {
			return severityRank(fs[i].Severity) < severityRank(fs[k].Severity)
		}
		return fs[i].ReportedAt.Before(fs[k].ReportedAt)
	})
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 100
	}
	start := q.Cursor
	if start > len(fs) {
		start = len(fs)
	}
	end := start + q.Limit
	if end > len(fs) {
		end = len(fs)
	}
	out := FindingResult{Findings: fs[start:end], Counts: map[string]int{}, OpenBlocking: j.BlockingOpen()}
	if q.Summary {
		out.Summary = j.FindingSummary(q.ReviewRound, time.Now().UTC())
	}
	for _, f := range fs {
		out.Counts[f.Severity+":"+f.Status]++
	}
	if end < len(fs) {
		out.NextCursor = end
	}
	return out
}
func severityRank(s string) int {
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
func (s *Service) Require(id string) (*domain.ReviewJob, error) {
	j, e := s.Get(id)
	if e != nil {
		return nil, fmt.Errorf("%w:%s", e, id)
	}
	return j, nil
}
