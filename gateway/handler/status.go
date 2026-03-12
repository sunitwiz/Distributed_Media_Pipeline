package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rendermesh/distributed-media-pipeline/gateway/service"
)

type StatusHandler struct {
	jobService *service.JobService
}

func NewStatusHandler(jobService *service.JobService) *StatusHandler {
	return &StatusHandler{jobService: jobService}
}

func (h *StatusHandler) Handle(c *gin.Context) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id is required"})
		return
	}

	resp, err := h.jobService.GetJobStatus(c.Request.Context(), jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}
