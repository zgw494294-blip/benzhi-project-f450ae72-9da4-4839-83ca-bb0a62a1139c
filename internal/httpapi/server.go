package httpapi

import (
	"bytes"
	"caption-delivery-qc/internal/application"
	"caption-delivery-qc/internal/domain"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
)

type Server struct {
	app    *application.Service
	mux    *http.ServeMux
	logger *log.Logger
}

func New(app *application.Service) *Server {
	s := &Server{app: app, mux: http.NewServeMux(), logger: log.Default()}
	s.routes()
	return s
}
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = withRequestID(r)
		w.Header().Set("X-Request-ID", requestID(r))
		s.mux.ServeHTTP(w, r)
	})
}
func (s *Server) routes() {
	s.mux.HandleFunc("/healthz", s.health)
	s.mux.HandleFunc("/api/v1/verify", s.batchVerify)
	s.mux.HandleFunc("/api/v1/review-jobs/verify", s.batchVerify)
	s.mux.HandleFunc("/api/v1/review-jobs", s.jobs)
	s.mux.HandleFunc("/api/v1/review-jobs/", s.jobSubpath)
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	write(w, 200, map[string]string{"status": "ok"})
}
func decode(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if len(b) > 1<<20 {
		return fmt.Errorf("%w:请求体超过1MB", domain.ErrValidation)
	}
	if len(bytes.TrimSpace(b)) > 0 && !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return fmt.Errorf("%w:Content-Type 必须为 application/json", domain.ErrValidation)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return fmt.Errorf("%w:JSON 正文只能包含一个对象", domain.ErrValidation)
	}
	return nil
}
func requireActor(r *http.Request) (string, error) {
	a := actor(r)
	if a == "anonymous" || strings.TrimSpace(a) == "" {
		return "", fmt.Errorf("%w:必须提供 X-Operator", domain.ErrValidation)
	}
	return a, nil
}
func actor(r *http.Request) string {
	a := r.Header.Get("X-Operator")
	if a == "" {
		a = "anonymous"
	}
	return a
}
func idem(r *http.Request) string { return r.Header.Get("Idempotency-Key") }
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func errStatus(e error) int {
	switch {
	case errors.Is(e, context.Canceled), errors.Is(e, context.DeadlineExceeded):
		return 499
	case errors.Is(e, domain.ErrNotFound):
		return 404
	case errors.Is(e, domain.ErrConflict), errors.Is(e, domain.ErrIdempotencyConflict), errors.Is(e, domain.ErrImmutableConflict):
		return 409
	case errors.Is(e, domain.ErrCandidateDigest), errors.Is(e, domain.ErrBaselineConflict), errors.Is(e, domain.ErrChecklistDigest):
		return 409
	case errors.Is(e, domain.ErrSnapshotNotFrozen):
		return 404
	case errors.Is(e, domain.ErrInvalidState), errors.Is(e, domain.ErrForbidden):
		return 422
	case errors.Is(e, domain.ErrValidation):
		return 400
	default:
		return 500
	}
}
func (s *Server) batchVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		write(w, http.StatusMethodNotAllowed, nil)
		return
	}
	var body struct {
		Credentials []domain.Credential `json:"credentials"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	out, err := s.app.VerifyCredentialsBatch(body.Credentials)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, out)
}
func fail(w http.ResponseWriter, e error) {
	write(w, errStatus(e), map[string]any{"code": application.Code(e), "error": e.Error()})
}
func atoi(v string) int { n, _ := strconv.Atoi(v); return n }
func (s *Server) jobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		write(w, 405, nil)
		return
	}
	var in application.CreateJobInput
	if e := decode(r, &in); e != nil && e != io.EOF {
		fail(w, e)
		return
	}
	in.Actor = actor(r)
	in.IdempotencyKey = idem(r)
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		fail(w, fmt.Errorf("%w:创建必须提供 Idempotency-Key", domain.ErrValidation))
		return
	}
	j, e := s.app.CreateJobContext(r.Context(), in)
	if e != nil {
		fail(w, e)
		return
	}
	write(w, 201, j)
}
func (s *Server) patchMetadata(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ExpectedVersion *int64  `json:"expectedVersion"`
		ProgramTitle    *string `json:"programTitle"`
		DurationMs      *int64  `json:"durationMs"`
		Language        *string `json:"language"`
		DeliveryBatch   *string `json:"deliveryBatch"`
		RuleSet         *string `json:"ruleSet"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, fmt.Errorf("%w:%v", domain.ErrValidation, err))
		return
	}
	operator, err := requireActor(r)
	if err != nil {
		fail(w, err)
		return
	}
	if body.ExpectedVersion == nil {
		fail(w, fmt.Errorf("%w:expectedVersion 不能为空", domain.ErrValidation))
		return
	}
	if body.ProgramTitle == nil && body.DurationMs == nil && body.Language == nil && body.DeliveryBatch == nil && body.RuleSet == nil {
		fail(w, fmt.Errorf("%w:至少提供一个元数据字段", domain.ErrValidation))
		return
	}
	key := strings.TrimSpace(idem(r))
	if key == "" {
		fail(w, fmt.Errorf("%w:元数据修订必须提供 Idempotency-Key", domain.ErrValidation))
		return
	}
	current, err := s.app.Get(id)
	if err != nil {
		fail(w, err)
		return
	}
	next := current.Metadata()
	if body.ProgramTitle != nil {
		next.ProgramTitle = *body.ProgramTitle
	}
	if body.DurationMs != nil {
		next.DurationMs = *body.DurationMs
	}
	if body.Language != nil {
		next.Language = *body.Language
	}
	if body.DeliveryBatch != nil {
		next.DeliveryBatch = *body.DeliveryBatch
	}
	if body.RuleSet != nil {
		next.RuleSet = *body.RuleSet
	}
	j, err := s.app.ReviseMetadata(id, next, *body.ExpectedVersion, operator, key)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, j)
}
func (s *Server) jobSubpath(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/review-jobs/"), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		write(w, 404, nil)
		return
	}
	id := parts[0]
	if len(parts) == 1 {
		if r.Method == "GET" {
			if revision := atoi(r.URL.Query().Get("revision")); revision > 0 {
				snapshot, e := s.app.Snapshot(id, revision, r.URL.Query().Get("candidateDigest"))
				if e != nil {
					fail(w, e)
					return
				}
				write(w, 200, snapshot)
				return
			}
			j, e := s.app.Detail(id)
			if e != nil {
				fail(w, e)
				return
			}
			write(w, 200, j)
			return
		}
		if r.Method == http.MethodPatch {
			s.patchMetadata(w, r, id)
			return
		}
		write(w, 405, nil)
		return
	}
	action := parts[1]
	var body struct {
		ExpectedVersion      int64                    `json:"expectedVersion"`
		Cue                  domain.Cue               `json:"cue"`
		Finding              domain.Finding           `json:"finding"`
		Note                 string                   `json:"note"`
		Revision             int                      `json:"revision"`
		StartMs              int64                    `json:"startMs"`
		EndMs                int64                    `json:"endMs"`
		Speaker              string                   `json:"speaker"`
		Text                 string                   `json:"text"`
		CueID                string                   `json:"cueID"`
		Category             string                   `json:"category"`
		Severity             string                   `json:"severity"`
		Evidence             string                   `json:"evidence"`
		Operations           []domain.CueEdit         `json:"operations"`
		Edits                []domain.CueEdit         `json:"edits"`
		Findings             []domain.Finding         `json:"findings"`
		FindingItems         []domain.Finding         `json:"items"`
		Responses            []domain.FindingResponse `json:"responses"`
		ImportIdempotencyKey string                   `json:"importIdempotencyKey"`
		CueContentHash       string                   `json:"cueContentHash"`
		RevisionFrom         int                      `json:"fromRevision"`
		RevisionTo           int                      `json:"toRevision"`
		CandidateRevision    int                      `json:"candidateRevision"`
		CandidateDigest      string                   `json:"candidateDigest"`
		SignNote             string                   `json:"signNote"`
		SigningNote          string                   `json:"signingNote"`
		SignatureNote        string                   `json:"signatureNote"`
		ReviewRound          int                      `json:"reviewRound"`
		CategoryFilter       string                   `json:"-"`
		Credential           *domain.Credential       `json:"credential,omitempty"`
		Credentials          []domain.Credential      `json:"credentials,omitempty"`
		Preflight            bool                     `json:"preflight,omitempty"`
		DryRun               bool                     `json:"dryRun,omitempty"`
		BaseRevision         int                      `json:"baseRevision,omitempty"`
		BaseDigest           string                   `json:"baseDigest,omitempty"`
		ConclusionNote       string                   `json:"conclusionNote,omitempty"`
		FindingSummaryDigest string                   `json:"findingSummaryDigest,omitempty"`
		ChecklistDigest      string                   `json:"checklistDigest,omitempty"`
	}
	if de := decode(r, &body); de != nil && de != io.EOF {
		fail(w, fmt.Errorf("%w:%v", domain.ErrValidation, de))
		return
	}
	if r.Method == "GET" {
		body.ExpectedVersion = int64(atoi(r.URL.Query().Get("expectedVersion")))
		body.Revision = atoi(r.URL.Query().Get("revision"))
		body.RevisionFrom = atoi(r.URL.Query().Get("fromRevision"))
		body.RevisionTo = atoi(r.URL.Query().Get("toRevision"))
		body.ImportIdempotencyKey = r.URL.Query().Get("importIdempotencyKey")
		body.CandidateDigest = r.URL.Query().Get("candidateDigest")
		body.ReviewRound = atoi(r.URL.Query().Get("reviewRound"))
	}
	if body.Cue.Text == "" && body.Text != "" {
		body.Cue = domain.Cue{StartMs: body.StartMs, EndMs: body.EndMs, Speaker: body.Speaker, Text: body.Text}
	}
	if body.Finding.CueID == "" && body.CueID != "" {
		body.Finding = domain.Finding{CueID: body.CueID, Category: body.Category, Severity: body.Severity, Evidence: body.Evidence}
	}
	var out any
	var e error
	switch action {
	case "cues":
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			write(w, http.StatusMethodNotAllowed, nil)
			return
		}
		if len(body.Operations) == 0 {
			body.Operations = body.Edits
		}
		if r.Method == "POST" && (body.Preflight || body.DryRun || r.URL.Query().Get("preflight") == "true" || len(parts) > 2 && parts[2] == "preflight") {
			out, e = s.app.PreviewCues(id, body.Operations, body.ExpectedVersion)
			break
		}
		if r.Method == "POST" && len(body.Operations) > 0 {
			importKey := body.ImportIdempotencyKey
			if importKey == "" {
				importKey = idem(r)
			}
			var j *domain.ReviewJob
			var res []domain.CueEditResult
			var ee error
			current, getErr := s.app.Get(id)
			if getErr != nil {
				e = getErr
				break
			}
			if current.Status == domain.StatusRework {
				j, res, ee = s.app.ReworkCues(id, body.Operations, body.ExpectedVersion, body.BaseRevision, body.BaseDigest, actor(r))
			} else {
				j, res, ee = s.app.BatchCues(id, body.Operations, body.ExpectedVersion, actor(r), importKey)
			}
			e = ee
			out = map[string]any{"job": j, "version": func() int64 {
				if j == nil {
					return 0
				}
				return j.Version
			}(), "results": res}
			break
		}
		if r.Method == "DELETE" && len(parts) > 2 {
			var j *domain.ReviewJob
			current, getErr := s.app.Get(id)
			if getErr != nil {
				e = getErr
			} else if current.Status == domain.StatusRework {
				j, _, e = s.app.ReworkCues(id, []domain.CueEdit{{Op: "delete", CueID: parts[2]}}, body.ExpectedVersion, body.BaseRevision, body.BaseDigest, actor(r))
			} else {
				j, e = s.app.DeleteCue(id, parts[2], body.ExpectedVersion, actor(r))
			}
			out = j
		} else {
			var j *domain.ReviewJob
			current, getErr := s.app.Get(id)
			if getErr != nil {
				e = getErr
			} else if current.Status == domain.StatusRework {
				op := "add"
				if _, exists := current.Cues[body.Cue.ID]; exists {
					op = "update"
				}
				j, _, e = s.app.ReworkCues(id, []domain.CueEdit{{Op: op, CueID: body.Cue.ID, Cue: body.Cue}}, body.ExpectedVersion, body.BaseRevision, body.BaseDigest, actor(r))
			} else {
				j, e = s.app.UpsertCue(id, body.Cue, body.ExpectedVersion, actor(r))
			}
			out = j
		}
	case "submit":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			write(w, http.StatusMethodNotAllowed, nil)
			return
		}
		if r.Method == "GET" || len(parts) > 2 && parts[2] == "preflight" {
			var p domain.PreflightReport
			var ee error
			if raw := r.URL.Query().Get("gapThresholdMs"); raw != "" {
				p, ee = s.app.Preflight(id, body.ExpectedVersion, int64(atoi(raw)))
			} else {
				p, ee = s.app.Preflight(id, body.ExpectedVersion)
			}
			e = ee
			out = p
			break
		}
		var j *domain.ReviewJob
		j, e = s.app.Submit(id, body.ExpectedVersion, actor(r))
		out = j
	case "preflight":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			write(w, http.StatusMethodNotAllowed, nil)
			return
		}
		var p domain.PreflightReport
		var ee error
		if raw := r.URL.Query().Get("gapThresholdMs"); raw != "" {
			p, ee = s.app.Preflight(id, body.ExpectedVersion, int64(atoi(raw)))
		} else {
			p, ee = s.app.Preflight(id, body.ExpectedVersion)
		}
		e = ee
		out = p
	case "findings":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			write(w, http.StatusMethodNotAllowed, nil)
			return
		}
		if len(body.Findings) == 0 {
			body.Findings = body.FindingItems
		}
		if r.Method == http.MethodGet && len(parts) > 2 && (parts[2] == "closure-preflight" || parts[2] == "preflight") {
			p, ee := s.app.ClosurePreflight(id, body.ExpectedVersion)
			e = ee
			out = p
			break
		}
		if r.Method == "GET" && len(parts) > 2 && parts[2] == "finish" {
			stats, digest, ee := s.app.FindingStatistics(id, body.ReviewRound)
			e = ee
			out = map[string]any{"findingSummary": stats, "findingSummaryDigest": digest}
			break
		}
		if r.Method == "GET" {
			q := application.FindingQuery{ReviewRound: atoi(r.URL.Query().Get("reviewRound")), CandidateRevision: atoi(r.URL.Query().Get("candidateRevision")), Category: r.URL.Query().Get("category"), Severity: r.URL.Query().Get("severity"), Status: r.URL.Query().Get("status"), Cursor: atoi(r.URL.Query().Get("cursor")), Limit: atoi(r.URL.Query().Get("limit")), Summary: r.URL.Query().Get("summary") == "true"}
			out, e = s.app.FindFindings(id, q)
			break
		}
		if r.Method == "POST" && len(body.Findings) > 0 {
			j, ee := s.app.BatchFindings(id, body.Findings, body.ExpectedVersion, actor(r))
			e = ee
			out = j
			break
		}
		if r.Method == "POST" && len(body.Responses) > 0 {
			j, ee := s.app.BatchRespond(id, body.Responses, body.ExpectedVersion, actor(r))
			e = ee
			out = j
			break
		}
		if len(parts) > 2 && (parts[2] == "closure-preflight" || parts[2] == "preflight") {
			p, ee := s.app.ClosurePreflight(id, body.ExpectedVersion)
			e = ee
			out = p
		} else if len(parts) > 3 && parts[2] != "response" && parts[3] == "response" {
			var j *domain.ReviewJob
			j, e = s.app.Respond(id, parts[2], body.Note, body.Revision, body.ExpectedVersion, actor(r), body.CueContentHash)
			out = j
		} else if len(parts) > 2 && parts[2] == "finish" {
			var j *domain.ReviewJob
			j, e = s.app.FinishReviewWithConclusion(id, body.ExpectedVersion, actor(r), body.ConclusionNote, body.FindingSummaryDigest)
			out = j
		} else if len(parts) > 2 && parts[2] == "response" {
			if len(parts) < 4 {
				write(w, 400, nil)
				return
			}
			var j *domain.ReviewJob
			j, e = s.app.Respond(id, parts[3], body.Note, body.Revision, body.ExpectedVersion, actor(r), body.CueContentHash)
			out = j
		} else {
			var j *domain.ReviewJob
			j, e = s.app.AddFinding(id, body.Finding, body.ExpectedVersion, actor(r))
			out = j
		}
	case "approval":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			write(w, http.StatusMethodNotAllowed, nil)
			return
		}
		if r.Method == "GET" || len(parts) > 2 && parts[2] == "readiness" {
			rev := body.Revision
			if rev == 0 {
				rev = body.CandidateRevision
			}
			p, ee := s.app.Readiness(id, body.ExpectedVersion, rev, actor(r))
			if body.CandidateDigest != "" {
				p, ee = s.app.Readiness(id, body.ExpectedVersion, rev, actor(r), body.CandidateDigest)
			}
			e = ee
			out = p
			break
		}
		var j *domain.ReviewJob
		note := body.SignNote
		if note == "" {
			note = body.SigningNote
		}
		if note == "" {
			note = body.SignatureNote
		}
		j, e = s.app.Approve(id, body.ExpectedVersion, actor(r), body.CandidateRevision, body.CandidateDigest, body.ChecklistDigest, note)
		out = j
	case "review":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			write(w, http.StatusMethodNotAllowed, nil)
			return
		}
		if r.Method == http.MethodGet {
			stats, digest, ee := s.app.FindingStatistics(id, body.ReviewRound)
			e = ee
			out = map[string]any{"findingSummary": stats, "findingSummaryDigest": digest}
		} else {
			var j *domain.ReviewJob
			j, e = s.app.FinishReviewWithConclusion(id, body.ExpectedVersion, actor(r), body.ConclusionNote, body.FindingSummaryDigest)
			out = j
		}
	case "credential":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			write(w, http.StatusMethodNotAllowed, nil)
			return
		}
		if r.Method == "GET" {
			c, ok := s.appCredential(id)
			if !ok {
				write(w, 404, nil)
				return
			}
			out = c
		} else {
			c, ee := s.app.Credential(id, body.ExpectedVersion, actor(r), idem(r))
			e = ee
			out = c
		}
	case "timeline":
		if r.Method != http.MethodGet {
			write(w, http.StatusMethodNotAllowed, nil)
			return
		}
		q := application.TimelineQuery{Cursor: int64(atoi(r.URL.Query().Get("cursor"))), Limit: atoi(r.URL.Query().Get("limit")), Type: r.URL.Query().Get("type"), Actor: r.URL.Query().Get("actor")}
		page, ee := s.app.TimelinePage(id, q)
		e = ee
		out = page
	case "verify":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			write(w, http.StatusMethodNotAllowed, nil)
			return
		}
		if len(body.Credentials) > 0 {
			out, e = s.app.VerifyCredentialsBatch(body.Credentials)
		} else if body.Credential != nil {
			out, e = s.app.VerifyCredentialCopy(id, *body.Credential)
		} else {
			ok, c, ee := s.app.VerifyCredential(id)
			e = ee
			out = map[string]any{"valid": ok, "credential": c}
		}
	case "diff":
		if r.Method != http.MethodGet {
			write(w, http.StatusMethodNotAllowed, nil)
			return
		}
		from, to := body.RevisionFrom, body.RevisionTo
		if len(parts) >= 4 {
			from, to = atoi(parts[2]), atoi(parts[3])
		}
		if from == 0 || to == 0 {
			write(w, 400, map[string]string{"error": "需要起止修订号"})
			return
		}
		d, ee := s.app.Diff(id, from, to)
		e = ee
		out = map[string]any{"fromRevision": from, "toRevision": to, "changes": d}
	default:
		write(w, 404, nil)
		return
	}
	if e != nil {
		fail(w, e)
		return
	}
	write(w, 200, out)
}
func (s *Server) appCredential(id string) (domain.Credential, bool) {
	d, e := s.app.Detail(id)
	if e != nil || d.Credential == nil {
		return domain.Credential{}, false
	}
	return *d.Credential, true
}
