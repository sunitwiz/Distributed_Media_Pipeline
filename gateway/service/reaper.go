package service

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/rendermesh/distributed-media-pipeline/gateway/store"
	"github.com/rendermesh/distributed-media-pipeline/shared/config"
)

type Reaper struct {
	store *store.RedisStore
	cfg   *config.Config
}

func NewReaper(store *store.RedisStore, cfg *config.Config) *Reaper {
	return &Reaper{store: store, cfg: cfg}
}

func (r *Reaper) Start(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.ReaperInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reapStaleJobs(ctx)
			r.reapStaleWorkers(ctx)
		}
	}
}

func (r *Reaper) reapStaleJobs(ctx context.Context) {
	jobIDs, err := r.store.GetProcessingJobs(ctx)
	if err != nil {
		log.Printf("reaper: failed to get processing jobs: %v", err)
		return
	}

	cutoff := time.Now().Add(-r.cfg.HeartbeatTimeout).Unix()

	for _, jobID := range jobIDs {
		job, err := r.store.GetJob(ctx, jobID)
		if err != nil {
			continue
		}

		if job["status"] != "processing" {
			continue
		}

		heartbeat, _ := strconv.ParseInt(job["last_heartbeat"], 10, 64)
		if heartbeat > cutoff {
			continue
		}

		retryCount, _ := strconv.Atoi(job["retry_count"])
		retryCount++

		if retryCount >= r.cfg.MaxRetries {
			log.Printf("reaper: job %s exceeded max retries, marking failed", jobID)
			r.store.UpdateJob(ctx, jobID, map[string]string{
				"status":      "failed",
				"error":       "max retries exceeded",
				"retry_count": strconv.Itoa(retryCount),
			})
			continue
		}

		log.Printf("reaper: re-queuing stale job %s (retry %d/%d)", jobID, retryCount, r.cfg.MaxRetries)
		r.store.UpdateJob(ctx, jobID, map[string]string{
			"status":      "pending",
			"worker_id":   "",
			"retry_count": strconv.Itoa(retryCount),
		})
		r.store.EnqueueJob(ctx, jobID)
	}
}

func (r *Reaper) reapStaleWorkers(ctx context.Context) {
	cutoff := time.Now().Add(-r.cfg.HeartbeatTimeout)
	if err := r.store.RemoveStaleWorkers(ctx, cutoff); err != nil {
		log.Printf("reaper: failed to remove stale workers: %v", err)
	}
}
