// 这个文件放三件"横切"的事：请求超时、每请求日志、CORS。
// 它们不属于任何一个端点，而是所有端点共用的外壳，所以单独一个文件。
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// contextWithTimeout 给"这次请求要做的数据库操作"设一个时间上限。
//
// 上限是叠在请求自带的 context 上，不是新建一个：请求自己的 context 在
// **客户端断开**时会自动取消。如果换成全新的 context，就丢掉这个信号——
// 浏览器已经刷新走了，后端还在把这次查询做完。
//
// 谁先到就取消谁：客户端断开，或者超时。下游的 SQL 收到取消信号会立刻放弃，
// 不会出现"页面早没人看了，进程还在排队查"的堆积。
func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

// statusRecorder 存在的理由只有一个：日志需要事后知道"这次响应是几"。
//
// HTTP 的写入器是单向的——写完状态码之后，没人会回头告诉你刚才写的是 200 还是 409。
// 所以这里把它包一层：WriteHeader 被调用时顺手把数字记下来，其余行为原样透传。
// 对下游 handler 来说它和普通的写入器没有区别。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader 在真正写出状态码之前先记一笔。
//
// 初始值必须是 200（见 withLogging）：如果 handler 一次都没显式写状态码，
// Go 会在第一次写响应体时隐式发 200。不预设 200 的话，日志里会出现 status=0 这种假状态。
func (s *statusRecorder) WriteHeader(code int) {
	s.status = code                    // 先记账
	s.ResponseWriter.WriteHeader(code) // 再交给真正的写入器
}

// withLogging 给每个请求打一行日志：方法、路径、状态码、耗时。
//
// 这行日志是和管理端那个「请求日志」面板对账用的：浏览器记"我发了什么、收到什么"，
// 服务端记"我收到什么、回了什么、花了多久"。两边时间戳一对，
// 就能判断问题卡在浏览器、网络，还是后端自己慢。
//
// 一个观察技巧：耗时本身能反推这次请求走到了哪一层——
// 400 在解码阶段就返回（耗时接近 0）、幂等的 PATCH 只做一次 SELECT（也接近 0）、
// 真正写库的那次才会出现毫秒级耗时。
func withLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()

		// 包一层写入器，才好知道这次响应最终是什么状态码
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		// 交给下一层：可能是另一个中间件，也可能是最终的业务 handler
		next.ServeHTTP(rec, r)

		// 到这里响应已经写完，只剩记录
		logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path, // 不带 query；路径足够定位是哪个端点
			"status", rec.status,
			"took", time.Since(started).Round(time.Microsecond).String(),
		)
	})
}

// withCORS 加跨域头，让"浏览器直连后端"这条路也能走通。
//
// 什么时候真需要它：
//   - 日常开发**不需要**——管理端走 vite 的 /api 代理，浏览器看到的都是同源请求
//   - 演示时想直接打开 dist 产物、或把页面放到另一个端口，就需要
//
// 放开 * 的前提是 v1 没有鉴权（题目没要求）。真部署必须换成白名单，
// 否则任何网站都能代替用户调这个接口。
//
// OPTIONS 分支处理"预检请求"：浏览器发非简单请求（比如带
// Content-Type: application/json 的 PATCH）之前，会先发一个 OPTIONS 问"允不允许"。
// 它没有业务语义，回 204 即可；不在这里拦掉，它就会一路落到兜底 handler 变成 404。
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
