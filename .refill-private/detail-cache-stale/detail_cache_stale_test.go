package detailcachestale_test

import (
	"bytes"
	"caption-delivery-qc/internal/application"
	"caption-delivery-qc/internal/domain"
	"caption-delivery-qc/internal/httpapi"
	"caption-delivery-qc/internal/journal"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func requestJSON(t *testing.T, client *http.Client, method, url, actor, idempotencyKey string, body, result any) int {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if actor != "" {
		req.Header.Set("X-Operator", actor)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			t.Fatal(err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode
}

func TestDetailCacheRefreshesAfterCueWrite(t *testing.T) {
	store, err := journal.New("")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(application.New(store)).Handler())
	defer server.Close()

	create := application.CreateJobInput{
		ProgramTitle:  "晚间新闻",
		DurationMs:    5000,
		Language:      "zh-CN",
		DeliveryBatch: "batch-2026-08-23",
		RuleSet:       "broadcast-v1",
	}
	var job domain.ReviewJob
	if status := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/review-jobs", "producer-a", "create-detail-cache-test", create, &job); status != http.StatusCreated {
		t.Fatalf("create status: %d", status)
	}

	var before application.Detail
	if status := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/review-jobs/"+job.ID, "", "", nil, &before); status != http.StatusOK {
		t.Fatalf("initial detail status: %d", status)
	}
	if before.Job.Version != 0 || len(before.Job.Cues) != 0 {
		t.Fatalf("unexpected initial detail: version=%d cues=%d", before.Job.Version, len(before.Job.Cues))
	}

	write := map[string]any{
		"expectedVersion": 0,
		"operations": []domain.CueEdit{{
			Op:    "add",
			CueID: "cue-1",
			Cue: domain.Cue{
				StartMs: 0,
				EndMs:   5000,
				Speaker: "主播",
				Text:    "这是用于交付审校的完整字幕",
			},
		}},
	}
	if status := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/review-jobs/"+job.ID+"/cues", "producer-a", "cue-import-1", write, nil); status != http.StatusOK {
		t.Fatalf("cue write status: %d", status)
	}

	var after application.Detail
	if status := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/review-jobs/"+job.ID, "", "", nil, &after); status != http.StatusOK {
		t.Fatalf("updated detail status: %d", status)
	}
	if after.Job.Version != 1 || len(after.Job.Cues) != 1 {
		t.Fatal("detail cache served the pre-write projection")
	}
}
