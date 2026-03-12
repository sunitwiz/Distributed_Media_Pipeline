package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rendermesh/distributed-media-pipeline/gateway/service"
)

type TranscodeHandler struct {
	jobService *service.JobService
}

func NewTranscodeHandler(jobService *service.JobService) *TranscodeHandler {
	return &TranscodeHandler{jobService: jobService}
}

type transcodeRequest struct {
	InputFilename string `json:"input_filename" binding:"required"`
}

func (h *TranscodeHandler) Handle(c *gin.Context) {
	var req transcodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "input_filename is required"})
		return
	}

	resp, err := h.jobService.CreateJob(c.Request.Context(), service.CreateJobRequest{
		Type:          "transcode",
		InputFilename: req.InputFilename,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, resp)
}
