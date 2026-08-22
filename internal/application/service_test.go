package application

import (
	"caption-delivery-qc/internal/journal"
	"testing"
)

func TestIdempotentCreate(t *testing.T) {
	st, _ := journal.New("")
	s := New(st)
	in := CreateJobInput{ProgramTitle: "节目", DurationMs: 1000, Language: "zh", DeliveryBatch: "b", RuleSet: "broadcast-v1", Actor: "p", IdempotencyKey: "same"}
	a, e := s.CreateJob(in)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.CreateJob(in)
	if e != nil {
		t.Fatal(e)
	}
	if a.ID != b.ID {
		t.Fatalf("idempotency returned %s and %s", a.ID, b.ID)
	}
}
