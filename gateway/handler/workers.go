package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rendermesh/distributed-media-pipeline/gateway/store"
)

type WorkersHandler struct {
	store *store.RedisStore
}

func NewWorkersHandler(store *store.RedisStore) *WorkersHandler {
	return &WorkersHandler{store: store}
}

type workerInfo struct {
	ID       string `json:"id"`
	LastSeen string `json:"last_seen"`
}

func (h *WorkersHandler) Handle(c *gin.Context) {
	members, err := h.store.GetActiveWorkers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	workers := make([]workerInfo, 0, len(members))
	for _, m := range members {
		ts := time.Unix(int64(m.Score), 0).UTC().Format(time.RFC3339)
		workers = append(workers, workerInfo{
			ID:       m.Member.(string),
			LastSeen: ts,
		})
	}

	c.JSON(http.StatusOK, gin.H{"workers": workers})
}
