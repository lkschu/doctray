package requestlog

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const loggerKey = "doctray.request_logger"

var requestSequence atomic.Uint64

func Middleware(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}

	return func(ctx *gin.Context) {
		requestLogger := logger.With("request_id", requestSequence.Add(1))
		ctx.Set(loggerKey, requestLogger)
		started := time.Now()
		ctx.Next()
		requestLogger.Info("http request completed",
			"event", "http.request.completed",
			"method", ctx.Request.Method,
			"path", ctx.Request.URL.Path,
			"status", ctx.Writer.Status(),
			"duration_ms", time.Since(started).Milliseconds(),
		)
	}
}

func FromGin(ctx *gin.Context) *slog.Logger {
	return FromGinOr(ctx, slog.Default())
}

func FromGinOr(ctx *gin.Context, fallback *slog.Logger) *slog.Logger {
	if logger, ok := ctx.Get(loggerKey); ok {
		if logger, ok := logger.(*slog.Logger); ok {
			return logger
		}
	}
	if fallback != nil {
		return fallback
	}
	return slog.Default()
}
