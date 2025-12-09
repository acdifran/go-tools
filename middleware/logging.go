package middleware

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/acdifran/go-tools/logger"
	"github.com/google/uuid"
)

type customResponseWriter struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func newCustomResponseWriter(w http.ResponseWriter) *customResponseWriter {
	return &customResponseWriter{w, http.StatusOK, bytes.NewBuffer(nil)}
}

func (crw *customResponseWriter) WriteHeader(statusCode int) {
	crw.statusCode = statusCode
	crw.ResponseWriter.WriteHeader(statusCode)
}

func (crw *customResponseWriter) Write(b []byte) (int, error) {
	crw.body.Write(b) // Capture the body
	return crw.ResponseWriter.Write(b)
}

func AddRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		startTime := time.Now()
		ctx = logger.AppendCtx(ctx, slog.String("request_id", uuid.NewString()))

		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.ErrorContext(ctx, "Error reading body", "error", err)
			http.Error(w, "can't read body", http.StatusBadRequest)
			return
		}

		requestLogger := logger.Default().WithGroup("request").With(
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("remote_addr", r.RemoteAddr),
			slog.String("user_agent", r.UserAgent()),
			slog.String("referer", r.Referer()),
			slog.Any("body", body),
		)
		requestLogger.InfoContext(ctx, "Request Started")
		crw := newCustomResponseWriter(w)
		r.Body = io.NopCloser(bytes.NewBuffer(body))
		next.ServeHTTP(
			crw,
			r.WithContext(ctx),
		)
		if crw.statusCode >= 400 {
			requestLogger.ErrorContext(
				ctx,
				crw.body.String(),
				slog.Int("response_code", crw.statusCode),
			)
		}
		endTime := time.Now()
		requestLogger.InfoContext(
			ctx,
			"Request Finished",
			slog.Int64("duration_ms", endTime.Sub(startTime).Milliseconds()),
			slog.Int("response_code", crw.statusCode),
		)
	})
}

func AddViewerToLogs(fromContext func(context.Context) any) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			vc := fromContext(ctx)
			ctx = logger.AppendCtx(ctx, slog.Any("viewer", vc))
			next.ServeHTTP(
				w,
				r.WithContext(ctx),
			)
		})
	}
}
