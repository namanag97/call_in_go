package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/namanag97/call_in_go/call-processor/domain"
	"github.com/namanag97/call_in_go/call-processor/ingestion"
	"github.com/namanag97/call_in_go/call-processor/repository"
	"github.com/namanag97/call_in_go/call-processor/transcription"
	"github.com/namanag97/call_in_go/call-processor/analysis"
)

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

// SuccessResponse represents a success response
type SuccessResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
}

// RecordingController handles recording-related API endpoints
type RecordingController struct {
	ingestionService *ingestion.Service
	recordingRepo    repository.RecordingRepository
}

// NewRecordingController creates a new RecordingController
func NewRecordingController(ingestionService *ingestion.Service, recordingRepo repository.RecordingRepository) *RecordingController {
	return &RecordingController{
		ingestionService: ingestionService,
		recordingRepo:    recordingRepo,
	}
}

// RegisterRoutes registers the controller's routes with the router
func (c *RecordingController) RegisterRoutes(router *gin.RouterGroup) {
	recordings := router.Group("/recordings")
	{
		recordings.POST("", c.UploadRecording)
		recordings.GET("", c.ListRecordings)
		recordings.GET("/:id", c.GetRecording)
		recordings.GET("/:id/download", c.DownloadRecording)
		recordings.DELETE("/:id", c.DeleteRecording)
	}
}

// UploadRecording handles file upload
func (c *RecordingController) UploadRecording(ctx *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := ctx.Get("userID")
	if !exists {
		userID = uuid.Nil // Use nil UUID if no user ID is available
	}

	// Parse multipart form
	err := ctx.Request.ParseMultipartForm(10 << 20) // 10 MB max
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Message: "Invalid multipart form",
		})
		return
	}

	// Get file
	file, header, err := ctx.Request.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_file",
			Message: "Failed to get file from form",
		})
		return
	}
	defer file.Close()

	// Parse metadata
	metadata := make(map[string]interface{})
	metadataStr := ctx.PostForm("metadata")
	if metadataStr != "" {
		err = json.Unmarshal([]byte(metadataStr), &metadata)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "invalid_metadata",
				Message: "Failed to parse metadata JSON",
			})
			return
		}
	}

	// Parse tags
	var tags []string
	tagsStr := ctx.PostForm("tags")
	if tagsStr != "" {
		err = json.Unmarshal([]byte(tagsStr), &tags)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, ErrorResponse{
				Error:   "invalid_tags",
				Message: "Failed to parse tags JSON",
			})
			return
		}
	}

	// Prepare upload request
	uploadReq := ingestion.UploadRequest{
		File:         file,
		FileHeader:   header,
		UserID:       userID.(uuid.UUID),
		Metadata:     metadata,
		Source:       ctx.PostForm("source"),
		Tags:         tags,
		CallbackURL:  ctx.PostForm("callbackUrl"),
	}

	// Process upload
	response, err := c.ingestionService.ProcessUpload(ctx, uploadReq)
	if err != nil {
		if errors.Is(err, ingestion.ErrDuplicateFile) {
			// Return 409 Conflict for duplicate file
			ctx.JSON(http.StatusConflict, SuccessResponse{
				Success: true,
				Data:    response,
			})
			return
		}

		// Handle other errors
		statusCode := http.StatusInternalServerError
		errorType := "upload_failed"

		switch {
		case errors.Is(err, ingestion.ErrInvalidFile):
			statusCode = http.StatusBadRequest
			errorType = "invalid_file"
		case errors.Is(err, ingestion.ErrFileTooBig):
			statusCode = http.StatusRequestEntityTooLarge
			errorType = "file_too_large"
		case errors.Is(err, ingestion.ErrUnsupportedType):
			statusCode = http.StatusUnsupportedMediaType
			errorType = "unsupported_file_type"
		}

		ctx.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusCreated, SuccessResponse{
		Success: true,
		Data:    response,
	})
}

// GetRecording retrieves a recording by ID
func (c *RecordingController) GetRecording(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid recording ID format",
		})
		return
	}

	recording, err := c.recordingRepo.Get(ctx, id)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "fetch_failed"

		if errors.Is(err, repository.ErrNotFound) {
			statusCode = http.StatusNotFound
			errorType = "not_found"
		}

		ctx.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: "Failed to fetch recording",
		})
		return
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    recording,
	})
}

// ListRecordings lists recordings with filtering and pagination
func (c *RecordingController) ListRecordings(ctx *gin.Context) {
	// Parse pagination parameters
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	pagination := domain.NewPagination(page, pageSize)

	// Parse filter parameters
	filter := domain.RecordingFilter{}

	// Status filter
	statusStr := ctx.Query("status")
	if statusStr != "" {
		// Split comma-separated statuses
		statusStrings := strings.Split(statusStr, ",")
		for _, s := range statusStrings {
			filter.Status = append(filter.Status, domain.RecordingStatus(s))
		}
	}

	// Source filter
	filter.Source = ctx.Query("source")

	// Created by filter
	createdByStr := ctx.Query("createdBy")
	if createdByStr != "" {
		createdByID, err := uuid.Parse(createdByStr)
		if err == nil {
			filter.CreatedBy = &createdByID
		}
	}

	// Tags filter
	tagsStr := ctx.Query("tags")
	if tagsStr != "" {
		filter.Tags = strings.Split(tagsStr, ",")
	}

	// Date range filter
	fromStr := ctx.Query("from")
	if fromStr != "" {
		fromTime, err := time.Parse(time.RFC3339, fromStr)
		if err == nil {
			filter.From = &fromTime
		}
	}

	toStr := ctx.Query("to")
	if toStr != "" {
		toTime, err := time.Parse(time.RFC3339, toStr)
		if err == nil {
			filter.To = &toTime
		}
	}

	// Search filter
	filter.Search = ctx.Query("search")

	// Fetch recordings
	recordings, total, err := c.recordingRepo.List(ctx, filter, pagination)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "fetch_failed",
			Message: "Failed to fetch recordings",
		})
		return
	}

	// Prepare response
	response := struct {
		Items      []*domain.Recording `json:"items"`
		Pagination struct {
			Page      int `json:"page"`
			PageSize  int `json:"pageSize"`
			TotalItems int `json:"totalItems"`
			TotalPages int `json:"totalPages"`
		} `json:"pagination"`
	}{
		Items: recordings,
		Pagination: struct {
			Page      int `json:"page"`
			PageSize  int `json:"pageSize"`
			TotalItems int `json:"totalItems"`
			TotalPages int `json:"totalPages"`
		}{
			Page:      pagination.Page,
			PageSize:  pagination.PageSize,
			TotalItems: total,
			TotalPages: (total + pagination.PageSize - 1) / pagination.PageSize,
		},
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    response,
	})
}

// DownloadRecording generates a presigned URL for downloading a recording
func (c *RecordingController) DownloadRecording(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid recording ID format",
		})
		return
	}

	// Get expiry parameter (default to 1 hour)
	expiresInStr := ctx.DefaultQuery("expiresIn", "3600")
	expiresIn, err := strconv.Atoi(expiresInStr)
	if err != nil || expiresIn <= 0 || expiresIn > 86400 {
		expiresIn = 3600 // Default to 1 hour, max 24 hours
	}

	// Generate presigned URL
	presignedURL, err := c.ingestionService.GetPresignedURL(ctx, id, time.Duration(expiresIn)*time.Second)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "download_failed"

		if errors.Is(err, repository.ErrNotFound) {
			statusCode = http.StatusNotFound
			errorType = "not_found"
		}

		ctx.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: "Failed to generate download URL",
		})
		return
	}

	// Prepare response
	response := struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expiresAt"`
	}{
		URL:       presignedURL,
		ExpiresAt: time.Now().Add(time.Duration(expiresIn) * time.Second),
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    response,
	})
}

// DeleteRecording deletes a recording
func (c *RecordingController) DeleteRecording(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid recording ID format",
		})
		return
	}

	err = c.recordingRepo.Delete(ctx, id)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "delete_failed"

		if errors.Is(err, repository.ErrNotFound) {
			statusCode = http.StatusNotFound
			errorType = "not_found"
		}

		ctx.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: "Failed to delete recording",
		})
		return
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    nil,
	})
}

// TranscriptionController handles transcription-related API endpoints
type TranscriptionController struct {
	transcriptionService *transcription.Service
	transcriptionRepo    repository.TranscriptionRepository
}

// NewTranscriptionController creates a new TranscriptionController
func NewTranscriptionController(transcriptionService *transcription.Service, transcriptionRepo repository.TranscriptionRepository) *TranscriptionController {
	return &TranscriptionController{
		transcriptionService: transcriptionService,
		transcriptionRepo:    transcriptionRepo,
	}
}

// RegisterRoutes registers the controller's routes with the router
func (c *TranscriptionController) RegisterRoutes(router *gin.RouterGroup) {
	transcriptions := router.Group("/transcriptions")
	{
		transcriptions.POST("", c.StartTranscription)
		transcriptions.GET("", c.ListTranscriptions)
		transcriptions.GET("/:id", c.GetTranscription)
		transcriptions.GET("/recording/:recordingId", c.GetTranscriptionByRecordingID)
	}
}

// StartTranscription starts the transcription process
func (c *TranscriptionController) StartTranscription(ctx *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := ctx.Get("userID")
	if !exists {
		userID = uuid.Nil // Use nil UUID if no user ID is available
	}

	// Parse request body
	var req struct {
		RecordingID string                 `json:"recordingId" binding:"required"`
		Language    string                 `json:"language"`
		Engine      string                 `json:"engine"`
		Metadata    map[string]interface{} `json:"metadata"`
		Priority    int                    `json:"priority"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Message: "Invalid request body",
		})
		return
	}

	// Parse recording ID
	recordingID, err := uuid.Parse(req.RecordingID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_recording_id",
			Message: "Invalid recording ID format",
		})
		return
	}

	// Prepare transcription request
	transcriptionReq := transcription.TranscriptionRequest{
		RecordingID: recordingID,
		Language:    req.Language,
		Engine:      req.Engine,
		UserID:      userID.(uuid.UUID),
		Metadata:    req.Metadata,
		Priority:    req.Priority,
	}

	// Start transcription
	response, err := c.transcriptionService.StartTranscription(ctx, transcriptionReq)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "transcription_failed"

		switch {
		case errors.Is(err, domain.ErrInvalidInput):
			statusCode = http.StatusBadRequest
			errorType = "invalid_input"
		case errors.Is(err, repository.ErrNotFound):
			statusCode = http.StatusNotFound
			errorType = "recording_not_found"
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

// GetTranscription gets a transcription by ID
func (c *TranscriptionController) GetTranscription(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid transcription ID format",
		})
		return
	}

	transcription, err := c.transcriptionService.GetTranscription(ctx, id)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "fetch_failed"

		if errors.Is(err, repository.ErrNotFound) {
			statusCode = http.StatusNotFound
			errorType = "not_found"
		}

		ctx.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: "Failed to fetch transcription",
		})
		return
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    transcription,
	})
}

// GetTranscriptionByRecordingID gets a transcription by recording ID
func (c *TranscriptionController) GetTranscriptionByRecordingID(ctx *gin.Context) {
	recordingIdStr := ctx.Param("recordingId")
	recordingId, err := uuid.Parse(recordingIdStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid recording ID format",
		})
		return
	}

	transcription, err := c.transcriptionService.GetTranscriptionByRecordingID(ctx, recordingId)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "fetch_failed"

		if errors.Is(err, repository.ErrNotFound) {
			statusCode = http.StatusNotFound
			errorType = "not_found"
		}

		ctx.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: "Failed to fetch transcription",
		})
		return
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    transcription,
	})
}

// ListTranscriptions lists transcriptions with filtering and pagination
func (c *TranscriptionController) ListTranscriptions(ctx *gin.Context) {
	// Parse pagination parameters
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	pagination := domain.NewPagination(page, pageSize)

	// Parse filter parameters
	filter := domain.TranscriptionFilter{}

	// Status filter
	statusStr := ctx.Query("status")
	if statusStr != "" {
		statusStrings := strings.Split(statusStr, ",")
		for _, s := range statusStrings {
			filter.Status = append(filter.Status, domain.TranscriptionStatus(s))
		}
	}

	// Recording ID filter
	recordingIdStr := ctx.Query("recordingId")
	if recordingIdStr != "" {
		recordingId, err := uuid.Parse(recordingIdStr)
		if err == nil {
			filter.RecordingID = &recordingId
		}
	}

	// Language filter
	filter.Language = ctx.Query("language")

	// Engine filter
	filter.Engine = ctx.Query("engine")

	// Date range filter
	fromStr := ctx.Query("from")
	if fromStr != "" {
		fromTime, err := time.Parse(time.RFC3339, fromStr)
		if err == nil {
			filter.From = &fromTime
		}
	}

	toStr := ctx.Query("to")
	if toStr != "" {
		toTime, err := time.Parse(time.RFC3339, toStr)
		if err == nil {
			filter.To = &toTime
		}
	}

	// Fetch transcriptions
	transcriptions, total, err := c.transcriptionRepo.List(ctx, filter, pagination)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "fetch_failed",
			Message: "Failed to fetch transcriptions",
		})
		return
	}

	// Prepare response
	response := struct {
		Items      []*domain.Transcription `json:"items"`
		Pagination struct {
			Page      int `json:"page"`
			PageSize  int `json:"pageSize"`
			TotalItems int `json:"totalItems"`
			TotalPages int `json:"totalPages"`
		} `json:"pagination"`
	}{
		Items: transcriptions,
		Pagination: struct {
			Page      int `json:"page"`
			PageSize  int `json:"pageSize"`
			TotalItems int `json:"totalItems"`
			TotalPages int `json:"totalPages"`
		}{
			Page:      pagination.Page,
			PageSize:  pagination.PageSize,
			TotalItems: total,
			TotalPages: (total + pagination.PageSize - 1) / pagination.PageSize,
		},
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    response,
	})
}

// AnalysisController handles analysis-related API endpoints
type AnalysisController struct {
	analysisService *analysis.Service
	analysisRepo    repository.AnalysisRepository
}

// NewAnalysisController creates a new AnalysisController
func NewAnalysisController(analysisService *analysis.Service, analysisRepo repository.AnalysisRepository) *AnalysisController {
	return &AnalysisController{
		analysisService: analysisService,
		analysisRepo:    analysisRepo,
	}
}

// RegisterRoutes registers the controller's routes with the router
func (c *AnalysisController) RegisterRoutes(router *gin.RouterGroup) {
	analyses := router.Group("/analyses")
	{
		analyses.POST("", c.StartAnalysis)
		analyses.GET("", c.ListAnalyses)
		analyses.GET("/:id", c.GetAnalysis)
		analyses.GET("/recording/:recordingId", c.ListAnalysesByRecordingID)
		analyses.GET("/recording/:recordingId/type/:analysisType", c.GetAnalysisByRecordingIDAndType)
	}
}

// StartAnalysis starts the analysis process
func (c *AnalysisController) StartAnalysis(ctx *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := ctx.Get("userID")
	if !exists {
		userID = uuid.Nil // Use nil UUID if no user ID is available
	}

	// Parse request body
	var req struct {
		RecordingID  string                 `json:"recordingId" binding:"required"`
		AnalysisType string                 `json:"analysisType" binding:"required"`
		Config       map[string]interface{} `json:"config"`
		Priority     int                    `json:"priority"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Message: "Invalid request body",
		})
		return
	}

	// Parse recording ID
	recordingID, err := uuid.Parse(req.RecordingID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_recording_id",
			Message: "Invalid recording ID format",
		})
		return
	}

	// Prepare analysis request
	analysisReq := analysis.AnalysisRequest{
		RecordingID:  recordingID,
		AnalysisType: analysis.AnalysisType(req.AnalysisType),
		UserID:       userID.(uuid.UUID),
		Priority:     req.Priority,
		Config:       req.Config,
	}

	// Start analysis
	response, err := c.analysisService.StartAnalysis(ctx, analysisReq)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "analysis_failed"

		switch {
		case errors.Is(err, domain.ErrInvalidInput):
			statusCode = http.StatusBadRequest
			errorType = "invalid_input"
		case errors.Is(err, repository.ErrNotFound):
			statusCode = http.StatusNotFound
			errorType = "recording_not_found"
		case errors.Is(err, analysis.ErrNoTranscription):
			statusCode = http.StatusBadRequest
			errorType = "no_transcription"
		case errors.Is(err, analysis.ErrUnsupportedType):
			statusCode = http.StatusBadRequest
			errorType = "unsupported_analysis_type"
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

// GetAnalysis gets an analysis by ID
func (c *AnalysisController) GetAnalysis(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid analysis ID format",
		})
		return
	}

	analysis, err := c.analysisService.GetAnalysis(ctx, id)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "fetch_failed"

		if errors.Is(err, repository.ErrNotFound) {
			statusCode = http.StatusNotFound
			errorType = "not_found"
		}

		ctx.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: "Failed to fetch analysis",
		})
		return
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    analysis,
	})
}

// GetAnalysisByRecordingIDAndType gets an analysis by recording ID and type
func (c *AnalysisController) GetAnalysisByRecordingIDAndType(ctx *gin.Context) {
	recordingIdStr := ctx.Param("recordingId")
	recordingId, err := uuid.Parse(recordingIdStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid recording ID format",
		})
		return
	}

	analysisType := ctx.Param("analysisType")
	if analysisType == "" {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_type",
			Message: "Analysis type is required",
		})
		return
	}

	analysis, err := c.analysisService.GetAnalysisByRecordingIDAndType(ctx, recordingId, analysisType)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "fetch_failed"

		if errors.Is(err, repository.ErrNotFound) {
			statusCode = http.StatusNotFound
			errorType = "not_found"
		}

		ctx.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: "Failed to fetch analysis",
		})
		return
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    analysis,
	})
}

// ListAnalysesByRecordingID lists all analyses for a recording
func (c *AnalysisController) ListAnalysesByRecordingID(ctx *gin.Context) {
	recordingIdStr := ctx.Param("recordingId")
	recordingId, err := uuid.Parse(recordingIdStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid recording ID format",
		})
		return
	}

	analyses, err := c.analysisService.ListAnalysesByRecordingID(ctx, recordingId)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "fetch_failed",
			Message: "Failed to fetch analyses",
		})
		return
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    analyses,
	})
}

// ListAnalyses lists analyses with filtering and pagination
func (c *AnalysisController) ListAnalyses(ctx *gin.Context) {
	// Parse pagination parameters
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	pagination := domain.NewPagination(page, pageSize)

	// Parse filter parameters
	filter := domain.AnalysisFilter{}

	// Status filter
	statusStr := ctx.Query("status")
	if statusStr != "" {
		statusStrings := strings.Split(statusStr, ",")
		for _, s := range statusStrings {
			filter.Status = append(filter.Status, domain.AnalysisStatus(s))
		}
	}

	// Recording ID filter
	recordingIdStr := ctx.Query("recordingId")
	if recordingIdStr != "" {
		recordingId, err := uuid.Parse(recordingIdStr)
		if err == nil {
			filter.RecordingID = &recordingId
		}
	}

	// Analysis type filter
	filter.AnalysisType = ctx.Query("analysisType")

	// Date range filter
	fromStr := ctx.Query("from")
	if fromStr != "" {
		fromTime, err := time.Parse(time.RFC3339, fromStr)
		if err == nil {
			filter.From = &fromTime
		}
	}

	toStr := ctx.Query("to")
	if toStr != "" {
		toTime, err := time.Parse(time.RFC3339, toStr)
		if err == nil {
			filter.To = &toTime
		}
	}

	// Fetch analyses
	analyses, total, err := c.analysisRepo.List(ctx, filter, pagination)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "fetch_failed",
			Message: "Failed to fetch analyses",
		})
		return
	}

	// Prepare response
	response := struct {
		Items      []*domain.Analysis `json:"items"`
		Pagination struct {
			Page      int `json:"page"`
			PageSize  int `json:"pageSize"`
			TotalItems int `json:"totalItems"`
			TotalPages int `json:"totalPages"`
		} `json:"pagination"`
	}{
		Items: analyses,
		Pagination: struct {
			Page      int `json:"page"`
			PageSize  int `json:"pageSize"`
			TotalItems int `json:"totalItems"`
			TotalPages int `json:"totalPages"`
		}{
			Page:      pagination.Page,
			PageSize:  pagination.PageSize,
			TotalItems: total,
			TotalPages: (total + pagination.PageSize - 1) / pagination.PageSize,
		},
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    response,
	})
}

// JobController handles job-related API endpoints
type JobController struct {
	workerManager worker.Manager
}

// NewJobController creates a new JobController
func NewJobController(workerManager worker.Manager) *JobController {
	return &JobController{
		workerManager: workerManager,
	}
}

// RegisterRoutes registers the controller's routes with the router
func (c *JobController) RegisterRoutes(router *gin.RouterGroup) {
	jobs := router.Group("/jobs")
	{
		jobs.GET("/:id", c.GetJob)
	}
}

// GetJob gets a job by ID
func (c *JobController) GetJob(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid job ID format",
		})
		return
	}

	job, err := c.workerManager.GetJobStatus(ctx, id)
	if err != nil {
		statusCode := http.StatusInternalServerError
		errorType := "fetch_failed"

		if errors.Is(err, repository.ErrNotFound) {
			statusCode = http.StatusNotFound
			errorType = "not_found"
		}

		ctx.JSON(statusCode, ErrorResponse{
			Error:   errorType,
			Message: "Failed to fetch job",
		})
		return
	}

	ctx.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data:    job,
	})
}