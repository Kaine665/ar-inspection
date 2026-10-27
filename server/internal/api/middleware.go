package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// contextWithTimeout 在请求的 context 上叠一个超时：客户端断开或超时都能让
// 下游 SQL 立刻收工，不会出现「浏览器刷新了，后端还在慢慢查」的堆积。
func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

// statusRecorder 记下实际写出的状态码，供日志中间件使用。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// withLogging 每个请求打一行：方法、路径、状态码、耗时。
// 这一行是和管理端「请求日志」对账用的——两边时间戳一对，就知道问题出在谁身上。
func withLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"took", time.Since(started).Round(time.Microsecond).String(),
		)
	})
}

// withCORS 只是为了让「直连后端」这条路也能走通（admin 用 vite 代理时用不到）。
// v1 没有鉴权，所以放开 * 没有额外风险；真部署时这里必须收回白名单。
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
