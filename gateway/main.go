package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/rendermesh/distributed-media-pipeline/gateway/handler"
	"github.com/rendermesh/distributed-media-pipeline/gateway/service"
	"github.com/rendermesh/distributed-media-pipeline/gateway/store"
	"github.com/rendermesh/distributed-media-pipeline/gateway/storage"
	"github.com/rendermesh/distributed-media-pipeline/shared/config"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	redisStore := store.NewRedisStore(cfg.RedisAddr)
	defer redisStore.Close()

	if err := redisStore.Ping(ctx); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}
	log.Println("connected to redis")

	minioStorage, err := storage.NewMinIOStorage(
		cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey, cfg.MinIOBucket, cfg.MinIOUseSSL,
	)
	if err != nil {
		log.Fatalf("failed to create minio client: %v", err)
	}

	if err := minioStorage.EnsureBucket(ctx); err != nil {
		log.Fatalf("failed to ensure minio bucket: %v", err)
	}
	log.Println("minio bucket ready")

	jobService := service.NewJobService(redisStore, minioStorage, cfg)

	reaper := service.NewReaper(redisStore, cfg)
	go reaper.Start(ctx)
	log.Println("reaper started")

	router := gin.Default()

	v1 := router.Group("/api/v1")
	{
		overlayH := handler.NewOverlayHandler(jobService)
		transcodeH := handler.NewTranscodeHandler(jobService)
		extractH := handler.NewExtractHandler(jobService)
		confirmH := handler.NewConfirmHandler(jobService)
		statusH := handler.NewStatusHandler(jobService)
		workersH := handler.NewWorkersHandler(redisStore)

		v1.POST("/jobs/overlay", overlayH.Handle)
		v1.POST("/jobs/transcode", transcodeH.Handle)
		v1.POST("/jobs/extract", extractH.Handle)
		v1.POST("/jobs/:id/confirm", confirmH.Handle)
		v1.GET("/jobs/:id", statusH.Handle)
		v1.GET("/workers", workersH.Handle)
	}

	healthH := handler.NewHealthHandler(redisStore)
	router.GET("/healthz", healthH.Handle)

	log.Printf("gateway starting on :%s", cfg.GatewayPort)
	go func() {
		if err := router.Run(":" + cfg.GatewayPort); err != nil {
			log.Fatalf("failed to start server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("gateway shutting down")
}
