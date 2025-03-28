package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/namanag97/call_in_go/call-processor/internal/bulk"
)

// BulkOperationsController handles bulk operation API endpoints
type BulkOperationsController struct {
	bulkService *bulk.Service
}

// NewBulkOperationsController creates a new BulkOperationsController
func NewBulkOperationsController(bulkService *bulk.Service) *BulkOperationsController {
	return &BulkOperationsController{
		bulkService: bulkService,
	}
}

// RegisterRoutes registers the controller's routes with the router
func (c *BulkOperationsController) RegisterRoutes(router *gin.RouterGroup) {
	bulkOperations := router.Group("/bulk")
	{
		bulkOperations.POST("/uploads", c.BulkUpload)
		bulkOperations.POST("/transcriptions", c.BulkTranscribe)
		bulkOperations.GET("/jobs/:id", c.GetBulkJobStatus)
	}
}

// BulkUpload handles bulk upload requests
func (c *BulkOperationsController) BulkUpload(ctx *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := ctx.Get("userID")
	if !exists {
		userID = uuid.Nil // Use nil UUID if no user ID is available
	}

	// Parse request body
	var req bulk.BulkUploadRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Message: "Invalid request body: " + err.Error(),
		})
		return
	}

	// Set user ID
	req.UserID = userID.(uuid.UUID)

	// Start bulk upload
	response, err := c.bulkService.StartBulkUpload(ctx, req)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "bulk_upload_failed"

		switch err {
		case bulk.ErrInvalidBulkRequest:
			statusCode = http.StatusBadRequest
			errorType = "invalid_request"
		}

		ctx.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusAccepted, SuccessResponse{
		Success: true,
		Data:    response,
	})
}

// BulkTranscribe handles bulk transcription requests
func (c *BulkOperationsController) BulkTranscribe(ctx *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := ctx.Get("userID")
	if !exists {
		userID = uuid.Nil // Use nil UUID if no user ID is available
	}

	// Parse request body
	var req bulk.BulkTranscribeRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Message: "Invalid request body: " + err.Error(),
		})
		return
	}

	// Set user ID
	req.UserID = userID.(uuid.UUID)

	// Start bulk transcription
	response, err := c.bulkService.StartBulkTranscribe(ctx, req)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "bulk_transcribe_failed"

		switch err {
		case bulk.ErrInvalidBulkRequest:
			statusCode = http.StatusBadRequest
			errorType = "invalid_request"
		}

		ctx.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusAccepted, SuccessResponse{
		Success: true,
		Data:    response,
	})
}

// GetBulkJobStatus gets the status of a bulk job
func (c *BulkOperationsController) GetBulkJobStatus(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid job ID format",
		})
		return
	}

	job, err := c.bulkService.GetBulkJobStatus(ctx, id)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "fetch_failed"

		if err.Error() == "entity not found" {
			statusCode = http.StatusNotFound
			errorType = "not_found"
		}

		ctx.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: "Failed to fetch job status",
		})
		return
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    job,
	})
}