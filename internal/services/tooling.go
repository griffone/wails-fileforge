package services

import (
	"context"
	"fmt"
	"log"

	"fileforge-desktop/internal/jobs"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

type ToolingService struct {
	ctx          context.Context
	registry     *registry.Registry
	orchestrator *jobs.Orchestrator
}

func NewToolingService(reg *registry.Registry) *ToolingService {
	if reg == nil {
		reg = registry.GetGlobalRegistry()
	}

	return &ToolingService{
		registry:     reg,
		orchestrator: jobs.NewOrchestrator(reg, 2),
	}
}

func (s *ToolingService) SetContext(ctx context.Context) {
	s.ctx = ctx
}

func (s *ToolingService) contextOrBackground() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *ToolingService) ListToolsV1() models.ListToolsResponseV1 {
	ctx := s.contextOrBackground()
	tools := s.registry.ListToolsV2(ctx)

	return models.ListToolsResponseV1{
		Success: true,
		Message: "tools listed successfully",
		Tools:   tools,
	}
}

func (s *ToolingService) ValidateJobV1(req models.JobRequestV1) models.ValidateJobResponseV1 {
	res := s.orchestrator.Validate(s.contextOrBackground(), req)
	if !res.Valid {
		code := ""
		if res.Error != nil {
			code = res.Error.Code
		}
		log.Printf("tooling.validate.invalid toolId=%s mode=%s errorCode=%s", req.ToolID, req.Mode, code)
	}
	return res
}

func (s *ToolingService) RunJobV1(req models.JobRequestV1) models.RunJobResponseV1 {
	res, err := s.orchestrator.Submit(s.contextOrBackground(), req)
	if err != nil {
		log.Printf("tooling.run.submit_failed toolId=%s mode=%s err=%v", req.ToolID, req.Mode, err)
		return models.RunJobResponseV1{
			Success: false,
			Message: "job submission failed",
			Status:  jobs.StatusFailed,
			Error:   models.NewCanonicalJobError("SUBMIT_ERROR", err.Error(), nil),
		}
	}

	if !res.Success {
		code := ""
		if res.Error != nil {
			code = res.Error.Code
		}
		log.Printf("tooling.run.rejected toolId=%s mode=%s errorCode=%s", req.ToolID, req.Mode, code)
	} else {
		log.Printf("tooling.run.submitted toolId=%s mode=%s jobId=%s", req.ToolID, req.Mode, res.JobID)
	}

	return res
}

func (s *ToolingService) CancelJobV1(jobID string) models.CancelJobResponseV1 {
	if err := s.orchestrator.Cancel(jobID); err != nil {
		log.Printf("tooling.cancel.failed jobId=%s err=%v", jobID, err)
		return models.CancelJobResponseV1{
			Success: false,
			Message: "cancel failed",
			JobID:   jobID,
			Error:   models.NewCanonicalJobError("NOT_FOUND", err.Error(), nil),
		}
	}

	log.Printf("tooling.cancel.requested jobId=%s", jobID)
	return models.CancelJobResponseV1{
		Success: true,
		Message: "cancel requested",
		JobID:   jobID,
	}
}

func (s *ToolingService) GetJobStatusV1(jobID string) models.JobStatusResponseV1 {
	job, found := s.orchestrator.GetJob(jobID)
	if !found {
		log.Printf("tooling.status.not_found jobId=%s", jobID)
		return models.JobStatusResponseV1{
			Success: false,
			Message: "job not found",
			Found:   false,
			Error:   models.NewCanonicalJobError("NOT_FOUND", fmt.Sprintf("job '%s' not found", jobID), nil),
		}
	}

	return models.JobStatusResponseV1{
		Success: true,
		Message: "job status retrieved",
		Found:   true,
		Result:  &job,
	}
}
