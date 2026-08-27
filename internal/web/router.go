package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gitlab.com/smoothsics/ainp/internal/config"
	"gitlab.com/smoothsics/ainp/internal/dashboard"
	"gitlab.com/smoothsics/ainp/internal/protocol"
	"gitlab.com/smoothsics/ainp/internal/service"
)

func NewRouter(cfg config.Config, eventService *service.EventService, logger *slog.Logger) http.Handler {
	return NewRouterWithDashboard(cfg, eventService, logger, nil)
}

func NewRouterWithDashboard(cfg config.Config, eventService *service.EventService, logger *slog.Logger, manager *dashboard.Manager) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(requestIDMiddleware())
	if cfg.Log.Access {
		router.Use(accessLogMiddleware(logger))
	}
	router.Use(gin.Recovery())
	router.GET("/healthz", func(ctx *gin.Context) { ctx.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	v1 := router.Group("/v1", authMiddleware(cfg.Auth.Token, logger))
	v1.POST("/check", func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
	v1.POST("/event", eventHandler(eventService, logger))
	if cfg.Admin.Enabled && manager != nil {
		registerDashboard(router, cfg, manager, logger)
	}
	return router
}

func registerDashboard(router *gin.Engine, cfg config.Config, manager *dashboard.Manager, logger *slog.Logger) {
	page, err := dashboard.HTML()
	if err != nil {
		panic(fmt.Errorf("load dashboard page: %w", err))
	}
	path := strings.TrimSuffix(cfg.Admin.Path, "/")
	router.GET(path, func(ctx *gin.Context) { ctx.Data(http.StatusOK, "text/html; charset=utf-8", page) })
	router.GET(path+"/", func(ctx *gin.Context) { ctx.Redirect(http.StatusTemporaryRedirect, path) })
	api := router.Group(path+"/api", authMiddleware(cfg.Auth.Token, logger))
	api.GET("/status", func(ctx *gin.Context) { ctx.JSON(http.StatusOK, manager.Status()) })
	api.GET("/report", func(ctx *gin.Context) {
		report, ok := manager.Report()
		if !ok {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "audit report is not available"})
			return
		}
		ctx.JSON(http.StatusOK, report)
	})
	api.POST("/refresh", func(ctx *gin.Context) {
		if !manager.Trigger() {
			ctx.JSON(http.StatusConflict, gin.H{"status": "already_running"})
			return
		}
		ctx.JSON(http.StatusAccepted, gin.H{"status": "started"})
	})
}

func eventHandler(eventService *service.EventService, logger *slog.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		requestID := ctx.GetString("request_id")
		var req protocol.EventRequest
		if err := decodeSingleJSON(ctx.Request.Body, &req); err != nil {
			logger.Warn("event_decode_error", "request_id", requestID, "error", err)
			writeError(ctx, http.StatusBadRequest, protocol.ErrorDecodeRequest, err.Error(), requestID)
			return
		}
		outcome := eventService.Apply(ctx.Request.Context(), req, requestID)
		if outcome.ErrorCode != "" {
			status := http.StatusBadRequest
			if outcome.ErrorCode == protocol.ErrorServer {
				status = http.StatusInternalServerError
			}
			writeError(ctx, status, outcome.ErrorCode, outcome.ErrorMessage, requestID)
			return
		}
		if outcome.AlreadyApplied {
			ctx.Status(http.StatusAlreadyReported)
			return
		}
		logger.Debug("event applied", "request_id", requestID, "seq_num", req.SeqNum)
		ctx.JSON(http.StatusOK, outcome.Response)
	}
}

func authMiddleware(expected string, logger *slog.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		provided := strings.TrimSpace(strings.TrimPrefix(ctx.GetHeader("Authorization"), "Bearer "))
		if provided == "" {
			provided = ctx.Query("access_token")
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			logger.Warn("auth_rejected", "request_id", ctx.GetString("request_id"), "path", ctx.FullPath())
			ctx.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		ctx.Next()
	}
}

func accessLogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		started := time.Now()
		ctx.Next()
		logger.Info("http_access",
			"request_id", ctx.GetString("request_id"),
			"method", ctx.Request.Method,
			"path", ctx.Request.URL.Path,
			"route", ctx.FullPath(),
			"status", ctx.Writer.Status(),
			"response_bytes", ctx.Writer.Size(),
			"latency_us", time.Since(started).Microseconds(),
		)
	}
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		requestID := ctx.GetHeader("X-Request-ID")
		if requestID == "" {
			var value [16]byte
			if _, err := rand.Read(value[:]); err == nil {
				requestID = hex.EncodeToString(value[:])
			} else {
				requestID = "request-id-unavailable"
			}
		}
		ctx.Set("request_id", requestID)
		ctx.Header("X-Request-ID", requestID)
		ctx.Next()
	}
}

func writeError(ctx *gin.Context, status int, code protocol.ErrorCode, message, requestID string) {
	ctx.JSON(status, protocol.ErrorResponse{ErrorCode: code, Message: &message, RequestID: requestID})
}

func decodeSingleJSON(body io.Reader, value any) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain a single JSON object")
		}
		return err
	}
	return nil
}
