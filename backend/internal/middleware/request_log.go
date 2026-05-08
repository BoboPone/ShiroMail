package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

const RequestIDHeader = "X-Request-ID"

func RequestID() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id := ctx.GetHeader(RequestIDHeader)
		if id == "" {
			id = generateRequestID()
		}
		ctx.Set("requestID", id)
		ctx.Header(RequestIDHeader, id)
		ctx.Next()
	}
}

func AccessLog() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		start := time.Now()
		ctx.Next()
		latency := time.Since(start)

		status := ctx.Writer.Status()
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}

		requestID, _ := ctx.Get("requestID")
		userID, _ := ctx.Get("auth.userID")

		slog.Log(ctx.Request.Context(), level, "http request",
			"method", ctx.Request.Method,
			"path", ctx.Request.URL.Path,
			"status", status,
			"latency_ms", latency.Milliseconds(),
			"ip", ctx.ClientIP(),
			"user_id", userID,
			"request_id", requestID,
			"user_agent", ctx.Request.UserAgent(),
		)
	}
}

func generateRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
