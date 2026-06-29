package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/appliedsymbolics/sigint/docs"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/appliedsymbolics/sigint/internal/api/controller"
	"github.com/appliedsymbolics/sigint/internal/api/service"
	"github.com/appliedsymbolics/sigint/internal/debugstream"
)

type Options struct {
	Service service.IngestService
	Debug   bool
	Auth    AuthOptions
	Replay  ReplayOptions
}

type AuthOptions struct {
	ProducerToken string
	InternalToken string
}

type ReplayOptions struct {
	DefaultLimit int
	MaxLimit     int
}

type Runtime struct {
	Router *gin.Engine
	Bus    *debugstream.Bus
}

func NewRouter(options Options) Runtime {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(
		gin.LoggerWithFormatter(formatRequestLog),
		gin.RecoveryWithWriter(prefixedWriter{writer: gin.DefaultErrorWriter, prefix: "panic: "}),
	)

	var bus *debugstream.Bus
	if options.Debug {
		bus = debugstream.NewBus()
	}
	ctrl := controller.New(options.Service, bus, controller.ReplayLimits{
		DefaultLimit: options.Replay.DefaultLimit,
		MaxLimit:     options.Replay.MaxLimit,
	})

	router.GET("/", func(ctx *gin.Context) {
		ctx.Redirect(http.StatusTemporaryRedirect, "/v1/docs/swagger/index.html")
	})
	router.GET("/llms.txt", llmsText)
	router.GET("/openapi.json", func(ctx *gin.Context) {
		ctx.Data(http.StatusOK, "application/json; charset=utf-8", []byte(docs.SwaggerInfo.ReadDoc()))
	})
	router.GET("/v1/docs/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	router.GET("/healthz", ctrl.Healthz)
	router.GET("/readyz", ctrl.Readyz)
	producerRoutes := router.Group("")
	if options.Auth.ProducerToken != "" {
		producerRoutes.Use(requireBearerToken(options.Auth.ProducerToken))
	}
	producerRoutes.POST("/v1/events", ctrl.IngestEvent)
	producerRoutes.POST("/v1/events:batch", ctrl.IngestBatch)
	router.GET("/v1/events/:event_id", ctrl.GetEvent)

	internalRoutes := router.Group("/internal/v1")
	if options.Auth.InternalToken != "" {
		internalRoutes.Use(requireBearerToken(options.Auth.InternalToken))
	}
	internalRoutes.GET("/events/replay", ctrl.ReplayEvents)

	if options.Debug {
		debugRoutes := router.Group("/debug")
		if token := debugAuthToken(options.Auth); token != "" {
			debugRoutes.Use(requireBearerToken(token))
		}
		debugRoutes.GET("", ctrl.DebugPage)
		debugRoutes.GET("/history", ctrl.DebugHistory)
		debugRoutes.DELETE("/history", ctrl.DebugClearHistory)
		debugRoutes.GET("/stream", ctrl.DebugStream)
	}

	return Runtime{Router: router, Bus: bus}
}

// llmsText godoc
// @Summary LLM service index
// @Description Returns a concise Markdown index for agents and LLM clients.
// @Tags docs
// @Produce text/markdown
// @Success 200 {string} string "Markdown service index"
// @Router /llms.txt [get]
func llmsText(ctx *gin.Context) {
	ctx.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(`# sigint

> sigint collects immutable application event facts, stores raw envelopes, and exposes lookup and replay APIs.

Use the OpenAPI document for request and response schemas. Producer and internal routes may require bearer-token authorization when tokens are configured.

## Docs

- [OpenAPI JSON](/openapi.json): Machine-readable API contract.
- [Swagger UI](/v1/docs/swagger/index.html): Interactive API documentation.

## Core Routes

- [Health](/healthz): Lightweight liveness check.
- [Readiness](/readyz): Dependency readiness check.
- [Event ingest](/v1/events): Store one validated event envelope.
- [Batch ingest](/v1/events:batch): Store a batch with per-event results.
- [Replay](/internal/v1/events/replay): Cursor-based replay of stored events.
`))
}

func requireBearerToken(expectedToken string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validBearerToken(ctx.GetHeader("Authorization"), expectedToken) {
			ctx.JSON(http.StatusUnauthorized, gin.H{"detail": "missing or invalid bearer token"})
			ctx.Abort()
			return
		}
		ctx.Next()
	}
}

func validBearerToken(header string, expectedToken string) bool {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	providedHash := sha256.Sum256([]byte(parts[1]))
	expectedHash := sha256.Sum256([]byte(expectedToken))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func debugAuthToken(auth AuthOptions) string {
	if auth.InternalToken != "" {
		return auth.InternalToken
	}
	return auth.ProducerToken
}

func formatRequestLog(param gin.LogFormatterParams) string {
	latency := param.Latency
	if latency > time.Minute {
		latency = latency.Truncate(time.Second)
	}

	line := fmt.Sprintf(
		"%s %s %3d | %13v | %15s | %s %s | %s\n",
		statusLabel(param.StatusCode),
		methodLabel(param.Method),
		param.StatusCode,
		latency,
		param.ClientIP,
		param.Method,
		param.Path,
		param.TimeStamp.Format(time.RFC3339),
	)
	if param.ErrorMessage == "" {
		return line
	}
	return strings.TrimSuffix(line, "\n") + " | error: " + param.ErrorMessage + "\n"
}

func statusLabel(statusCode int) string {
	switch {
	case statusCode >= http.StatusInternalServerError:
		return "5xx"
	case statusCode >= http.StatusBadRequest:
		return "4xx"
	default:
		return "ok"
	}
}

func methodLabel(method string) string {
	switch method {
	case http.MethodGet:
		return "GET"
	case http.MethodPost:
		return "POST"
	case http.MethodPut:
		return "PUT"
	case http.MethodPatch:
		return "PATCH"
	case http.MethodDelete:
		return "DELETE"
	default:
		return "OTHER"
	}
}

type prefixedWriter struct {
	writer io.Writer
	prefix string
}

func (w prefixedWriter) Write(p []byte) (int, error) {
	return w.writer.Write([]byte(w.prefix + string(p)))
}
