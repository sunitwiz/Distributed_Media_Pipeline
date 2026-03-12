package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(addr string) *RedisStore {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &RedisStore{client: client}
}

func (s *RedisStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *RedisStore) Close() error {
	return s.client.Close()
}

func (s *RedisStore) CreateJob(ctx context.Context, jobID string, fields map[string]string, ttl time.Duration) error {
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, jobKey(jobID), fieldsToAny(fields))
	pipe.Expire(ctx, jobKey(jobID), ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisStore) GetJob(ctx context.Context, jobID string) (map[string]string, error) {
	result, err := s.client.HGetAll(ctx, jobKey(jobID)).Result()
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}
	return result, nil
}

func (s *RedisStore) UpdateJob(ctx context.Context, jobID string, fields map[string]string) error {
	return s.client.HSet(ctx, jobKey(jobID), fieldsToAny(fields)).Err()
}

func (s *RedisStore) PersistJob(ctx context.Context, jobID string) error {
	return s.client.Persist(ctx, jobKey(jobID)).Err()
}

func (s *RedisStore) EnqueueJob(ctx context.Context, jobID string) error {
	return s.client.LPush(ctx, "jobs:queue", jobID).Err()
}

func (s *RedisStore) DequeueJob(ctx context.Context, timeout time.Duration) (string, error) {
	result, err := s.client.BRPop(ctx, timeout, "jobs:queue").Result()
	if err != nil {
		return "", err
	}
	return result[1], nil
}

func (s *RedisStore) SetHeartbeat(ctx context.Context, jobID string, workerID string) error {
	now := strconv.FormatInt(time.Now().Unix(), 10)
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, jobKey(jobID), "last_heartbeat", now)
	pipe.ZAdd(ctx, "workers:active", redis.Z{Score: float64(time.Now().Unix()), Member: workerID})
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisStore) RegisterWorker(ctx context.Context, workerID string) error {
	return s.client.ZAdd(ctx, "workers:active", redis.Z{
		Score:  float64(time.Now().Unix()),
		Member: workerID,
	}).Err()
}

func (s *RedisStore) UnregisterWorker(ctx context.Context, workerID string) error {
	return s.client.ZRem(ctx, "workers:active", workerID).Err()
}

func (s *RedisStore) GetActiveWorkers(ctx context.Context) ([]redis.Z, error) {
	return s.client.ZRangeWithScores(ctx, "workers:active", 0, -1).Result()
}

func (s *RedisStore) GetStaleWorkers(ctx context.Context, before time.Time) ([]string, error) {
	result, err := s.client.ZRangeByScore(ctx, "workers:active", &redis.ZRangeBy{
		Min: "-inf",
		Max: strconv.FormatInt(before.Unix(), 10),
	}).Result()
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *RedisStore) RemoveStaleWorkers(ctx context.Context, before time.Time) error {
	return s.client.ZRemRangeByScore(ctx, "workers:active",
		"-inf",
		strconv.FormatInt(before.Unix(), 10),
	).Err()
}

func (s *RedisStore) GetProcessingJobs(ctx context.Context) ([]string, error) {
	var cursor uint64
	var jobIDs []string
	for {
		keys, nextCursor, err := s.client.Scan(ctx, cursor, "job:*", 100).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			status, err := s.client.HGet(ctx, key, "status").Result()
			if err != nil {
				continue
			}
			if status == "processing" {
				jobIDs = append(jobIDs, key[4:]) // strip "job:" prefix
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return jobIDs, nil
}

func jobKey(id string) string {
	return "job:" + id
}

func fieldsToAny(fields map[string]string) map[string]interface{} {
	result := make(map[string]interface{}, len(fields))
	for k, v := range fields {
		result[k] = v
	}
	return result
}
