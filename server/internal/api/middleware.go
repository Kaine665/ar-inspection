// 这个文件放"横切"的事：请求 id、panic 兜底、请求超时、每请求日志、CORS。
// 它们不属于任何一个端点，而是所有端点共用的外壳，所以单独一个文件。
//
// 套在外面的顺序（router.go 里那行 Handler()）是：
//
//	CORS → 请求 id → 日志 → panic 兜底 → 路由
//
// 顺序不是随便排的：id 必须最先拿到（日志和 panic 都要用它），日志要在最外层
// 才能记到"最终写出去的状态码"，而 panic 兜底必须包住 mux，否则 mux 里 panic 了就没人接。
//
// 请求 id 的存在理由只有一个：**没有共同标识，两边的日志对不起来**。
// 管理端记"我发了什么、收到什么"，服务端记"我收到什么、回了什么"，
// 只靠时间戳对账在并发下必然失效（同一毫秒可能有好几个请求）。
// 有了 id，一个请求在两端就是同一行。
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"
)

// ctxKeyRequestID 是请求 id 在 context 里的键。
// 用自定义类型而不是字符串常量，避免和别的包塞进 context 的键撞名。
type ctxKeyRequestID struct{}

// requestIDFrom 取出当前请求的 id；取不到返回空串（不会 panic，调用方不必判空）。
func requestIDFrom(r *http.Request) string {
	if id, ok := r.Context().Value(ctxKeyRequestID{}).(string); ok {
		return id
	}
	return ""
}

// validRequestID 只接受 1–64 位的字母数字、连字符、下划线。
//
// 为什么要校验上游传来的 id：它会进日志、也可能被回显在响应头里。
// 不校验就等于允许调用方往我们的日志里写任意内容（换行可以伪造日志行）。
func validRequestID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// newRequestID 生成 12 字节随机十六进制串。
//
// 不用数据库那个 ULID：id 只需要"唯一且短"，不需要有序；
// 而且这条路径在每次请求上都走，越便宜越好（crypto/rand 足够快）。
func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 熵源坏了属于极端情况，退化成时间戳——仍然比"没有 id"好
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

// withRequestID 给每个请求定一个 id：上游带了合法值就沿用（方便跨服务串联），
// 没带或带了脏值就自己生成。id 回写进响应头，客户端可以把它带进自己的日志。
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID{}, id)))
	})
}

// withRecovery 接住 handler 里的 panic，把它变成一次正常的 500 响应。
//
// 没有这一层时，一个 nil 解引用会让这条连接被直接掐断：客户端拿到的是
// "connection reset"，不是契约里那种能解析的错误体——正是"沉默的出错"里最坏的一种。
// 有了它，panic 变成一条带堆栈的日志 + 一个标准的 internal 错误。
//
// 一个诚实的边界：如果 panic 发生在响应已经开始写出之后（状态码和部分 body 已经发走），
// HTTP 协议不允许再改状态码。这时只能记日志，没法把响应"修好"——注意是"记下来"
// 而不是假装没发生。
func withRecovery(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			logger.Error("handler panic",
				"requestId", requestIDFrom(r),
				"method", r.Method,
				"path", r.URL.Path,
				"panic", v,
				"stack", string(debug.Stack()),
			)
			if sr, ok := w.(*statusRecorder); ok && sr.wroteHeader {
				logger.Error("响应已经开始写出，panic 无法再改成 500", "requestId", requestIDFrom(r))
				return
			}
			writeJSON(w, http.StatusInternalServerError,
				errorEnvelope{Error: errorBody{Code: "internal", Message: "服务异常，请重试"}})
		}()
		next.ServeHTTP(w, r)
	})
}

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
	status      int
	wroteHeader bool // 状态码是否已经真写出去了（panic 兜底靠它判断"还能不能改"）
}

// WriteHeader 在真正写出状态码之前先记一笔。
//
// 初始值必须是 200（见 withLogging）：如果 handler 一次都没显式写状态码，
// Go 会在第一次写响应体时隐式发 200。不预设 200 的话，日志里会出现 status=0 这种假状态。
func (s *statusRecorder) WriteHeader(code int) {
	s.status = code      // 先记账
	s.wroteHeader = true // 再标记"已经写出"
	s.ResponseWriter.WriteHeader(code)
}

// Write 补上 wroteHeader 的隐式情形：handler 没调 WriteHeader 就直接写 body 时，
// Go 会隐式发 200。少了这个方法，panic 兜底会误以为"还没写过"，试图再写一次状态码。
func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
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
			"requestId", requestIDFrom(r), // 和管理端请求日志面板对账用的同一个 id
			"method", r.Method,
			"path", r.URL.Path, // 不带 query；路径足够定位是哪个端点
			"status", rec.status,
			"took", time.Since(started).Round(time.Microsecond).String(),
		)
	})
}

// withCORS 按配置加跨域头，让"浏览器直连后端"这条路也能走通。
//
// origin 的三种取值（由 config.CORSOrigin 决定，见 internal/config）：
//   - ""：**一个头都不发**，直接放行到内层。管理端与后端同源部署时就走这条，
//     浏览器本来就同源，不需要 CORS；这也顺带避免了"任何网站都能调接口"。
//   - "*"：全放开。只该用于 dev —— 前提是 v1 没有鉴权（题目没要求）且库里是测试数据。
//   - "https://admin.example.com"：白名单里的一行，生产推荐这种。
//
// OPTIONS 分支处理"预检请求"：浏览器发非简单请求（比如带
// Content-Type: application/json 的 PATCH）之前，会先发一个 OPTIONS 问"允不允许"。
// 它没有业务语义，回 204 即可；不在这里拦掉，它就会一路落到兜底 handler 变成 404。
// 注意：不开跨域时（origin == ""）也就不该替浏览器答预检，OPTIONS 会照常走到路由层拿 405。
func withCORS(origin string, next http.Handler) http.Handler {
	if origin == "" {
		return next // 同源部署：不参与跨域协商
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		// X-Request-Id 既要允许带上（客户端可以自己传），也要暴露出去
		// （否则浏览器里的 JS 读不到这个响应头，跨域时默认只有少数几个头可见）
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-Id")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-Id")
		// 响应内容随 Origin 变化时必须有 Vary，否则缓存会把 A 站点的响应发给 B 站点
		if origin != "*" {
			w.Header().Add("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
