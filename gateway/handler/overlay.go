package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rendermesh/distributed-media-pipeline/gateway/service"
)

type OverlayHandler struct {
	jobService *service.JobService
}

func NewOverlayHandler(jobService *service.JobService) *OverlayHandler {
	return &OverlayHandler{jobService: jobService}
}

type overlayRequest struct {
	InputFilename    string `json:"input_filename" binding:"required"`
	SubtitleFilename string `json:"subtitle_filename" binding:"required"`
}

func (h *OverlayHandler) Handle(c *gin.Context) {
	var req overlayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "input_filename and subtitle_filename are required"})
		return
	}

	resp, err := h.jobService.CreateJob(c.Request.Context(), service.CreateJobRequest{
		Type:             "overlay",
		InputFilename:    req.InputFilename,
		SubtitleFilename: req.SubtitleFilename,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, resp)
}
