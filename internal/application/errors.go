package application

import (
	"caption-delivery-qc/internal/domain"
	"context"
	"errors"
)

type ErrorCode string

const (
	CodeNotFound        ErrorCode = "not_found"
	CodeConflict        ErrorCode = "version_conflict"
	CodeInvalid         ErrorCode = "invalid_request"
	CodeState           ErrorCode = "invalid_state"
	CodeForbidden       ErrorCode = "forbidden"
	CodeIdempotency     ErrorCode = "idempotency_conflict"
	CodeImmutable       ErrorCode = "immutable_conflict"
	CodeIntegrity       ErrorCode = "integrity_error"
	CodeNotFrozen       ErrorCode = "candidate_not_frozen"
	CodeCandidateDigest ErrorCode = "candidate_digest_conflict"
	CodeBaseline        ErrorCode = "rework_baseline_conflict"
	CodeChecklist       ErrorCode = "checklist_digest_conflict"
	CodeContext         ErrorCode = "context_canceled"
)

func Code(err error) ErrorCode {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return CodeContext
	case errors.Is(err, domain.ErrNotFound):
		return CodeNotFound
	case errors.Is(err, domain.ErrConflict):
		return CodeConflict
	case errors.Is(err, domain.ErrForbidden):
		return CodeForbidden
	case errors.Is(err, domain.ErrInvalidState):
		return CodeState
	case errors.Is(err, domain.ErrIdempotencyConflict):
		return CodeIdempotency
	case errors.Is(err, domain.ErrImmutableConflict):
		return CodeImmutable
	case errors.Is(err, domain.ErrIntegrity):
		return CodeIntegrity
	case errors.Is(err, domain.ErrSnapshotNotFrozen):
		return CodeNotFrozen
	case errors.Is(err, domain.ErrCandidateDigest):
		return CodeCandidateDigest
	case errors.Is(err, domain.ErrBaselineConflict):
		return CodeBaseline
	case errors.Is(err, domain.ErrChecklistDigest):
		return CodeChecklist
	default:
		return CodeInvalid
	}
}
