package application

import "caption-delivery-qc/internal/domain"

type CueCommand struct {
	JobID           string
	Cue             domain.Cue
	ExpectedVersion int64
	Actor           string
}
type FindingCommand struct {
	JobID           string
	Finding         domain.Finding
	ExpectedVersion int64
	Actor           string
}
type ApprovalCommand struct {
	JobID           string
	ExpectedVersion int64
	Actor           string
}
type MetadataRevisionCommand struct {
	JobID           string
	Metadata        domain.MetadataRevision
	ExpectedVersion int64
	Actor           string
	IdempotencyKey  string
}

func (s *Service) AddCueCommand(c CueCommand) (*domain.ReviewJob, error) {
	return s.UpsertCue(c.JobID, c.Cue, c.ExpectedVersion, c.Actor)
}
func (s *Service) AddFindingCommand(c FindingCommand) (*domain.ReviewJob, error) {
	return s.AddFinding(c.JobID, c.Finding, c.ExpectedVersion, c.Actor)
}
func (s *Service) ApproveCommand(c ApprovalCommand) (*domain.ReviewJob, error) {
	return s.Approve(c.JobID, c.ExpectedVersion, c.Actor)
}
func (s *Service) ReviseMetadataCommand(c MetadataRevisionCommand) (*domain.ReviewJob, error) {
	return s.ReviseMetadata(c.JobID, c.Metadata, c.ExpectedVersion, c.Actor, c.IdempotencyKey)
}
