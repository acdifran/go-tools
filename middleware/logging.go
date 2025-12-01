package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/acdifran/go-tools/logger"
	"github.com/acdifran/go-tools/viewer"
	"github.com/google/uuid"
)

func AddRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		startTime := time.Now()
		ctx = logger.AppendCtx(ctx, slog.String("request_id", uuid.NewString()))

		requestLogger := logger.Default().WithGroup("request").With(
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("remote_addr", r.RemoteAddr),
			slog.String("user_agent", r.UserAgent()),
			slog.String("referer", r.Referer()),
		)
		requestLogger.InfoContext(ctx, "Request Started")
		next.ServeHTTP(
			w,
			r.WithContext(ctx),
		)
		endTime := time.Now()
		requestLogger.InfoContext(
			ctx,
			"Request Finished",
			slog.Int64("duration_ms", endTime.Sub(startTime).Milliseconds()),
		)
	})
}

func AddViewerToLogs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		vc := viewer.FromContext(ctx)
		ctx = logger.AppendCtx(ctx, slog.Any("viewer", vc))
		next.ServeHTTP(
			w,
			r.WithContext(ctx),
		)
	})
}
