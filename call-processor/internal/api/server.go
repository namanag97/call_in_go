package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// Config contains configuration for the API server
type Config struct {
	Port                 string
	BasePath             string
	AllowedOrigins       []string
	RequestSizeLimit     int64
	EnableSwagger        bool
	EnableMetrics        bool
	EnableTracing        bool
	TracingServiceName   string
}

// Server represents the API server
type Server struct {
	config      Config
	router      *gin.Engine
	controllers []Controller
}

// Controller defines the interface for API controllers
type Controller interface {
	RegisterRoutes(router *gin.RouterGroup)
}

// NewServer creates a new API server
func NewServer(config Config, controllers ...Controller) *Server {
	// Create router
	router := gin.New()

	// Use middleware
	router.Use(gin.Recovery())
	router.Use(LoggerMiddleware())
	
	// Configure CORS
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowOrigins = config.AllowedOrigins
	corsConfig.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Content-Length", "Accept-Encoding", "Authorization", "X-Request-ID"}
	corsConfig.ExposeHeaders = []string{"Content-Length", "Content-Type"}
	corsConfig.AllowCredentials = true
	corsConfig.MaxAge = 12 * time.Hour
	router.Use(cors.New(corsConfig))
	
	// Configure request size limit
	router.MaxMultipartMemory = config.RequestSizeLimit
	
	// Configure path prefix
	var basePath string
	if config.BasePath != "" {
		basePath = strings.TrimRight(config.BasePath, "/")
	}
	
	// Create API group
	baseGroup := router.Group(basePath)
	
	// Enable tracing if configured
	if config.EnableTracing {
		baseGroup.Use(otelgin.Middleware(config.TracingServiceName))
	}
	
	// Enable Swagger if configured
	if config.EnableSwagger {
		baseGroup.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}
	
	// Enable Prometheus metrics if configured
	if config.EnableMetrics {
		baseGroup.GET("/metrics", gin.WrapH(promhttp.Handler()))
	}
	
	// Health check endpoint
	baseGroup.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "healthy",
			"time":   time.Now().Format(time.RFC3339),
		})
	})
	
	// API version group
	apiGroup := baseGroup.Group("/api/v1")
	
	// Register controllers
	server := &Server{
		config:      config,
		router:      router,
		controllers: controllers,
	}
	
	for _, controller := range controllers {
		controller.RegisterRoutes(apiGroup)
	}
	
	return server
}

// Start starts the API server
func (s *Server) Start() error {
	// Start server
	return s.router.Run(":" + s.config.Port)
}

// GetRouter returns the Gin router
func (s *Server) GetRouter() *gin.Engine {
	return s.router
}

// LoggerMiddleware defines a custom logger middleware
func LoggerMiddleware() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		// Format: time | status | latency | method | path | client IP | error
		return fmt.Sprintf("%s | %3d | %13v | %6s | %s | %s | %s\n",
			param.TimeStamp.Format("2006/01/02 15:04:05"),
			param.StatusCode,
			param.Latency,
			param.Method,
			param.Path,
			param.ClientIP,
			param.ErrorMessage,
		)
	})
}

// RequestIDMiddleware adds a unique request ID to each request
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get existing request ID from header or generate a new one
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		
		// Set request ID in context and header
		c.Set("requestID", requestID)
		c.Header("X-Request-ID", requestID)
		
		c.Next()
	}
}

// ErrorHandlerMiddleware handles errors centrally
func ErrorHandlerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		
		// Check if there are any errors
		if len(c.Errors) > 0 {
			// Handle the error - could have different strategies based on error type
			err := c.Errors.Last().Err
			
			statusCode := http.StatusInternalServerError
			errorResponse := ErrorResponse{
				Error:   "internal_server_error",
				Message: "An unexpected error occurred",
			}
			
			// Check error type and set appropriate status code and response
			// This would be expanded based on actual error types in the application
			
			c.JSON(statusCode, errorResponse)
		}
	}
}

// MockAuthMiddleware is a placeholder for authentication - replace with real implementation
func MockAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// In a real implementation, validate the token, check permissions, etc.
		// For now, just set a dummy user ID
		c.Set("userID", uuid.MustParse("00000000-0000-0000-0000-000000000000"))
		c.Next()
	}
}

// OpenAPI spec for Swagger documentation
// @title Call Recording Processing API
// @version 1.0
// @description API for processing call recordings
// @termsOfService http://swagger.io/terms/
// @contact.name API Support
// @contact.email support@yourorganization.com
// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html
// @host localhost:8080
// @BasePath /api/v1
// @schemes http https
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization