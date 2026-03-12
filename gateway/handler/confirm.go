package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rendermesh/distributed-media-pipeline/gateway/service"
)

type ConfirmHandler struct {
	jobService *service.JobService
}

func NewConfirmHandler(jobService *service.JobService) *ConfirmHandler {
	return &ConfirmHandler{jobService: jobService}
}

func (h *ConfirmHandler) Handle(c *gin.Context) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job id is required"})
		return
	}

	if err := h.jobService.ConfirmUpload(c.Request.Context(), jobID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"job_id": jobID, "status": "pending"})
}
