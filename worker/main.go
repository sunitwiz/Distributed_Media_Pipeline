package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
	"github.com/rendermesh/distributed-media-pipeline/shared/config"
	"github.com/rendermesh/distributed-media-pipeline/worker/heartbeat"
	"github.com/rendermesh/distributed-media-pipeline/worker/processor"
)

func main() {
	cfg := config.Load()

	if cfg.WorkerID == "" {
		cfg.WorkerID = "worker-" + uuid.New().String()[:8]
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}
	log.Printf("[%s] connected to redis", cfg.WorkerID)

	minioClient, err := minio.New(cfg.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""),
		Secure: cfg.MinIOUseSSL,
	})
	if err != nil {
		log.Fatalf("failed to create minio client: %v", err)
	}
	log.Printf("[%s] connected to minio", cfg.WorkerID)

	redisClient.ZAdd(ctx, "workers:active", redis.Z{
		Score:  float64(time.Now().Unix()),
		Member: cfg.WorkerID,
	})

	hb := heartbeat.New(redisClient, cfg.WorkerID, cfg.HeartbeatInterval)

	go func() {
		ticker := time.NewTicker(cfg.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				redisClient.ZAdd(ctx, "workers:active", redis.Z{
					Score:  float64(time.Now().Unix()),
					Member: cfg.WorkerID,
				})
			}
		}
	}() 

	log.Printf("[%s] waiting for jobs...", cfg.WorkerID)
	for {
		select {
		case <-ctx.Done():
			log.Printf("[%s] shutting down", cfg.WorkerID)
			redisClient.ZRem(context.Background(), "workers:active", cfg.WorkerID)
			return
		default:
		}

		result, err := redisClient.BRPop(ctx, 5*time.Second, "jobs:queue").Result()
		if err != nil {
			if err == redis.Nil || ctx.Err() != nil {
				continue
			}
			log.Printf("[%s] dequeue error: %v", cfg.WorkerID, err)
			continue
		}

		jobID := result[1]
		log.Printf("[%s] picked up job %s", cfg.WorkerID, jobID)
		processJob(ctx, redisClient, minioClient, hb, cfg, jobID)
	}
}

func processJob(ctx context.Context, rc *redis.Client, mc *minio.Client, hb *heartbeat.Heartbeat, cfg *config.Config, jobID string) {
	job, err := rc.HGetAll(ctx, "job:"+jobID).Result()
	if err != nil || len(job) == 0 {
		log.Printf("[%s] failed to read job %s: %v", cfg.WorkerID, jobID, err)
		return
	}

	jobType := job["type"]
	if jobType != "overlay" && jobType != "transcode" && jobType != "extract" {
		failJob(ctx, rc, cfg.WorkerID, jobID, fmt.Sprintf("unknown job type: %s", jobType))
		return
	}

	if job["input_key"] == "" {
		failJob(ctx, rc, cfg.WorkerID, jobID, "missing input_key")
		return
	}

	rc.HSet(ctx, "job:"+jobID, map[string]interface{}{
		"status":    "processing",
		"worker_id": cfg.WorkerID,
	})

	stopHeartbeat := hb.Start(ctx, jobID)
	defer stopHeartbeat()

	workDir := filepath.Join(os.TempDir(), "rendermesh", jobID)
	os.MkdirAll(workDir, 0755)
	defer os.RemoveAll(workDir)

	inputPath := filepath.Join(workDir, filepath.Base(job["input_key"]))
	if err := mc.FGetObject(ctx, cfg.MinIOBucket, job["input_key"], inputPath, minio.GetObjectOptions{}); err != nil {
		failJob(ctx, rc, cfg.WorkerID, jobID, fmt.Sprintf("failed to download input: %v", err))
		return
	}
	log.Printf("[%s] downloaded input for job %s", cfg.WorkerID, jobID)

	var subtitlePath string
	if jobType == "overlay" {
		subtitleKey := job["subtitle_key"]
		if subtitleKey == "" {
			failJob(ctx, rc, cfg.WorkerID, jobID, "overlay job missing subtitle_key")
			return
		}
		subtitlePath = filepath.Join(workDir, filepath.Base(subtitleKey))
		if err := mc.FGetObject(ctx, cfg.MinIOBucket, subtitleKey, subtitlePath, minio.GetObjectOptions{}); err != nil {
			failJob(ctx, rc, cfg.WorkerID, jobID, fmt.Sprintf("failed to download subtitle: %v", err))
			return
		}
		log.Printf("[%s] downloaded subtitle for job %s", cfg.WorkerID, jobID)
	}

	var outputPath string
	switch jobType {
	case "overlay":
		outputPath, err = processor.RunOverlay(workDir, inputPath, subtitlePath)
	case "transcode":
		outputPath, err = processor.RunTranscode(workDir, inputPath)
	case "extract":
		outputPath, err = processor.RunExtract(workDir, inputPath)
	}

	if err != nil {
		failJob(ctx, rc, cfg.WorkerID, jobID, err.Error())
		return
	}
	log.Printf("[%s] ffmpeg completed for job %s", cfg.WorkerID, jobID)

	outputKey := job["output_key"]
	if _, err := mc.FPutObject(ctx, cfg.MinIOBucket, outputKey, outputPath, minio.PutObjectOptions{}); err != nil {
		failJob(ctx, rc, cfg.WorkerID, jobID, fmt.Sprintf("failed to upload output: %v", err))
		return
	}
	log.Printf("[%s] uploaded output for job %s", cfg.WorkerID, jobID)

	rc.HSet(ctx, "job:"+jobID, map[string]interface{}{
		"status":       "completed",
		"completed_at": time.Now().UTC().Format(time.RFC3339),
	})
	log.Printf("[%s] job %s completed", cfg.WorkerID, jobID)
}

func failJob(ctx context.Context, rc *redis.Client, workerID, jobID, errMsg string) {
	log.Printf("[%s] job %s failed: %s", workerID, jobID, errMsg)
	rc.HSet(ctx, "job:"+jobID, map[string]interface{}{
		"status": "failed",
		"error":  errMsg,
	})
}
