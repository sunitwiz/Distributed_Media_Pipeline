package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rendermesh/distributed-media-pipeline/gateway/store"
)

type HealthHandler struct {
	store *store.RedisStore
}

func NewHealthHandler(store *store.RedisStore) *HealthHandler {
	return &HealthHandler{store: store}
}

func (h *HealthHandler) Handle(c *gin.Context) {
	if err := h.store.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}
