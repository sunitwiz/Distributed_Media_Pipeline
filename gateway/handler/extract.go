package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rendermesh/distributed-media-pipeline/gateway/service"
)

type ExtractHandler struct {
	jobService *service.JobService
}

func NewExtractHandler(jobService *service.JobService) *ExtractHandler {
	return &ExtractHandler{jobService: jobService}
}

type extractRequest struct {
	InputFilename string `json:"input_filename" binding:"required"`
}

func (h *ExtractHandler) Handle(c *gin.Context) {
	var req extractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "input_filename is required"})
		return
	}

	resp, err := h.jobService.CreateJob(c.Request.Context(), service.CreateJobRequest{
		Type:          "extract",
		InputFilename: req.InputFilename,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, resp)
}
