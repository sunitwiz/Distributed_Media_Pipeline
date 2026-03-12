package heartbeat

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Heartbeat struct {
	client   *redis.Client
	workerID string
	interval time.Duration
}

func New(client *redis.Client, workerID string, interval time.Duration) *Heartbeat {
	return &Heartbeat{client: client, workerID: workerID, interval: interval}
}

func (h *Heartbeat) Start(ctx context.Context, jobID string) context.CancelFunc {
	hbCtx, cancel := context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(h.interval)
		defer ticker.Stop()

		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				now := strconv.FormatInt(time.Now().Unix(), 10)
				pipe := h.client.Pipeline()
				pipe.HSet(hbCtx, "job:"+jobID, "last_heartbeat", now)
				pipe.ZAdd(hbCtx, "workers:active", redis.Z{
					Score:  float64(time.Now().Unix()),
					Member: h.workerID,
				})
				if _, err := pipe.Exec(hbCtx); err != nil {
					if hbCtx.Err() != nil {
						return
					}
					log.Printf("heartbeat error for job %s: %v", jobID, err)
				}
			}
		}
	}()

	return cancel
}
