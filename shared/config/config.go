package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	RedisAddr        string
	MinIOEndpoint       string
	MinIOPublicEndpoint string
	MinIOAccessKey      string
	MinIOSecretKey   string
	MinIOBucket      string
	MinIOUseSSL      bool
	GatewayPort      string
	UploadURLExpiry  time.Duration
	JobTTL           time.Duration
	DownloadExpiry   time.Duration
	HeartbeatInterval time.Duration
	HeartbeatTimeout time.Duration
	ReaperInterval   time.Duration
	MaxRetries       int
	WorkerID         string
}

func Load() *Config {
	return &Config{
		RedisAddr:        getEnv("REDIS_ADDR", "redis:6379"),
		MinIOEndpoint:       getEnv("MINIO_ENDPOINT", "minio:9000"),
		MinIOPublicEndpoint: getEnv("MINIO_PUBLIC_ENDPOINT", "localhost:9000"),
		MinIOAccessKey:      getEnv("MINIO_ACCESS_KEY", "minioadmin"),
		MinIOSecretKey:   getEnv("MINIO_SECRET_KEY", "minioadmin"),
		MinIOBucket:      getEnv("MINIO_BUCKET", "media-pipeline"),
		MinIOUseSSL:      getEnvBool("MINIO_USE_SSL", false),
		GatewayPort:      getEnv("GATEWAY_PORT", "8080"),
		UploadURLExpiry:  getDuration("UPLOAD_URL_EXPIRY", 15*time.Minute),
		JobTTL:           getDuration("JOB_TTL", 2*time.Hour),
		DownloadExpiry:   getDuration("DOWNLOAD_EXPIRY", 1*time.Hour),
		HeartbeatInterval: getDuration("HEARTBEAT_INTERVAL", 5*time.Second),
		HeartbeatTimeout: getDuration("HEARTBEAT_TIMEOUT", 30*time.Second),
		ReaperInterval:   getDuration("REAPER_INTERVAL", 15*time.Second),
		MaxRetries:       getEnvInt("MAX_RETRIES", 3),
		WorkerID:         getEnv("WORKER_ID", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
