package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rendermesh/distributed-media-pipeline/gateway/store"
	"github.com/rendermesh/distributed-media-pipeline/gateway/storage"
	"github.com/rendermesh/distributed-media-pipeline/shared/config"
)

type JobService struct {
	store   *store.RedisStore
	storage *storage.MinIOStorage
	cfg     *config.Config
}

func NewJobService(store *store.RedisStore, storage *storage.MinIOStorage, cfg *config.Config) *JobService {
	return &JobService{store: store, storage: storage, cfg: cfg}
}

type CreateJobRequest struct {
	Type             string
	InputFilename    string
	SubtitleFilename string
}

type CreateJobResponse struct {
	JobID      string            `json:"job_id"`
	Status     string            `json:"status"`
	UploadURLs map[string]string `json:"upload_urls"`
}

type JobStatusResponse struct {
	JobID       string `json:"job_id"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	WorkerID    string `json:"worker_id,omitempty"`
	Error       string `json:"error,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
}

func (s *JobService) CreateJob(ctx context.Context, req CreateJobRequest) (*CreateJobResponse, error) {
	jobID := uuid.New().String()
	inputKey := fmt.Sprintf("inputs/%s/%s", jobID, req.InputFilename)

	fields := map[string]string{
		"type":       req.Type,
		"status":     "waiting_for_upload",
		"input_key":  inputKey,
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"retry_count": "0",
	}

	outputExt := ".mp4"
	if req.Type == "extract" {
		outputExt = ".mp3"
	}
	fields["output_key"] = fmt.Sprintf("outputs/%s/output%s", jobID, outputExt)

	uploadURLs := map[string]string{}

	videoURL, err := s.storage.GenerateUploadURL(ctx, inputKey, s.cfg.UploadURLExpiry)
	if err != nil {
		return nil, fmt.Errorf("failed to generate video upload url: %w", err)
	}
	uploadURLs["video"] = videoURL

	if req.Type == "overlay" && req.SubtitleFilename != "" {
		subtitleKey := fmt.Sprintf("inputs/%s/%s", jobID, req.SubtitleFilename)
		fields["subtitle_key"] = subtitleKey
		subtitleURL, err := s.storage.GenerateUploadURL(ctx, subtitleKey, s.cfg.UploadURLExpiry)
		if err != nil {
			return nil, fmt.Errorf("failed to generate subtitle upload url: %w", err)
		}
		uploadURLs["subtitle"] = subtitleURL
	}

	if err := s.store.CreateJob(ctx, jobID, fields, s.cfg.JobTTL); err != nil {
		return nil, fmt.Errorf("failed to create job record: %w", err)
	}

	return &CreateJobResponse{
		JobID:      jobID,
		Status:     "waiting_for_upload",
		UploadURLs: uploadURLs,
	}, nil
}

func (s *JobService) ConfirmUpload(ctx context.Context, jobID string) error {
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if job["status"] != "waiting_for_upload" {
		return fmt.Errorf("job %s is not waiting for upload, current status: %s", jobID, job["status"])
	}

	exists, err := s.storage.ObjectExists(ctx, job["input_key"])
	if err != nil {
		return fmt.Errorf("failed to check input file: %w", err)
	}
	if !exists {
		return fmt.Errorf("input file not found in storage: %s", job["input_key"])
	}

	if job["type"] == "overlay" {
		if subtitleKey, ok := job["subtitle_key"]; ok && subtitleKey != "" {
			exists, err := s.storage.ObjectExists(ctx, subtitleKey)
			if err != nil {
				return fmt.Errorf("failed to check subtitle file: %w", err)
			}
			if !exists {
				return fmt.Errorf("subtitle file not found in storage: %s", subtitleKey)
			}
		}
	}

	if err := s.store.PersistJob(ctx, jobID); err != nil {
		return fmt.Errorf("failed to persist job: %w", err)
	}

	if err := s.store.UpdateJob(ctx, jobID, map[string]string{"status": "pending"}); err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	if err := s.store.EnqueueJob(ctx, jobID); err != nil {
		return fmt.Errorf("failed to enqueue job: %w", err)
	}

	return nil
}

func (s *JobService) GetJobStatus(ctx context.Context, jobID string) (*JobStatusResponse, error) {
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	resp := &JobStatusResponse{
		JobID:       jobID,
		Type:        job["type"],
		Status:      job["status"],
		CreatedAt:   job["created_at"],
		CompletedAt: job["completed_at"],
		WorkerID:    job["worker_id"],
		Error:       job["error"],
	}

	if job["status"] == "completed" && job["output_key"] != "" {
		downloadURL, err := s.storage.GenerateDownloadURL(ctx, job["output_key"], s.cfg.DownloadExpiry)
		if err == nil {
			resp.DownloadURL = downloadURL
		}
	}

	return resp, nil
}
