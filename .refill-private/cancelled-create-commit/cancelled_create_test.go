package cancelledcreate

import (
	"caption-delivery-qc/internal/application"
	"caption-delivery-qc/internal/httpapi"
	"caption-delivery-qc/internal/journal"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type cancelAfterFirstCheck struct {
	context.Context
	calls atomic.Int32
}

func (c *cancelAfterFirstCheck) Err() error {
	if c.calls.Add(1) == 1 {
		return nil
	}
	return context.Canceled
}

func TestCancelledCreateDoesNotCommit(t *testing.T) {
	store, err := journal.New("")
	if err != nil {
		t.Fatal(err)
	}
	server := httpapi.New(application.New(store)).Handler()
	ctx := &cancelAfterFirstCheck{Context: context.Background()}
	body, err := json.Marshal(map[string]any{
		"programTitle":  "取消测试节目",
		"durationMs":    5000,
		"language":      "zh-CN",
		"deliveryBatch": "batch-cancel",
		"ruleSet":       "broadcast-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/review-jobs", strings.NewReader(string(body))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Operator", "producer")
	req.Header.Set("Idempotency-Key", "cancelled-create")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code == http.StatusCreated {
		t.Fatalf("cancelled request committed successfully: %s", rec.Body.String())
	}
	page, err := store.PageEvents(journal.EventQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 0 {
		t.Fatalf("cancelled request left %d committed event(s)", len(page.Events))
	}
}
