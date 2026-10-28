// 这个文件放三件"横切"的事：请求超时、每请求日志、CORS。
// 它们不属于任何一个端点，而是所有端点共用的外壳，所以单独一个文件。
package api

import (
	"context"  // context：传递"取消 / 超时"信号的机制
	"log/slog" // 结构化日志（Go 1.21+ 标准库）
	"net/http"
	"time"
)

// contextWithTimeout 在请求自带的 context 上，再叠一层超时。
//
// 为什么是"叠"而不是"新建"：
// 请求本身已经带了一个 context（r.Context()），**客户端断开**时它会自动取消。
// 如果 new 一个全新的 context，就丢掉了这层信号——浏览器刷新了，后端还在傻算。
// context.WithTimeout 表示"父 context 取消、或者我自己的截止时间到了，谁先到都取消"。
//
// 返回 (ctx, cancel)：
//   - ctx 交给下游（这里就是 SQL 调用）
//   - cancel 必须由调用方 defer 掉，否则 context 占用的定时器要等到超时才释放
func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

// statusRecorder 是"包装过的 ResponseWriter"，用来在事后知道响应码是几。
//
// 为什么需要它：net/http 的 ResponseWriter 是单向的——你只能往里写状态码，
// 写完之后没人会告诉你"刚才写的是 200 还是 409"。日志想打 status=…，
// 就只能自己拦一道：把 WriteHeader 包起来，顺手把数字记下来。
//
// 两个语法点：
//   - `http.ResponseWriter` 这一行只有类型、没有字段名，叫**嵌入（embedding）**：
//     自动获得该接口的全部方法，我们只覆盖想改的那一个。
//   - 正因为嵌入了，statusRecorder 自己也满足 http.ResponseWriter 接口，
//     可以当普通 ResponseWriter 交给后面的 handler，对它们完全透明。
type statusRecorder struct {
	http.ResponseWriter     // 嵌入真正的写入器，Header/Write 等方法原样透传
	status              int // 已经写出去的状态码
}

// WriteHeader 覆盖嵌入的同名方法：先记账，再真正写。
//
// 调用时机是 handler 第一次写状态码时。注意：如果 handler 一次都没显式调用
// WriteHeader，Go 在第一次写 body 时会隐式发 200——所以 withLogging 里
// status 的初始值必须是 200，否则日志会出现 status=0 这种假状态。
func (s *statusRecorder) WriteHeader(code int) {
	s.status = code                    // 先记账
	s.ResponseWriter.WriteHeader(code) // 再交给真正的写入器
}

// withLogging 是"日志中间件"：包住下一个 handler，进去记时间，回来打一行日志。
//
// 这是 Go 中间件的标准形状：
//
//	func 中间件(额外参数, next http.Handler) http.Handler {
//	    return http.HandlerFunc(func(w, r) {
//	        ...before...
//	        next.ServeHTTP(w, r)
//	        ...after...
//	    })
//	}
//
// http.HandlerFunc 是标准库的小技巧：把"普通函数"包装成"实现了 Handler 接口的类型"，
// 因为这个函数签名正好就是 ServeHTTP 的签名，所以能把闭包当 Handler 返回。
//
// 这行日志是和管理端「请求日志」面板对账用的：浏览器记"我发了什么、收到什么"，
// 服务端记"我收到什么、回了什么、花了多久"，两边时间戳一对就知道卡在谁身上。
//
// 一个观察技巧：耗时能反推这次请求走到了哪一层——
// 400 在解码阶段就返回（0s）、幂等的 PATCH 只做一次 SELECT（0s）、
// 真正写库的那次才会出现毫秒级耗时。
func withLogging(logger *slog.Logger, next http.Handler) http.Handler {
	// 返回一个闭包：它捕获了 logger 和 next，每次有请求进来就执行一次
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()

		// 把真正的 w 包一层，以便事后读 status（初始值 200 的原因见上面 WriteHeader）
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		// 交给下一层：可能是另一个中间件，也可能是最终 handler
		next.ServeHTTP(rec, r)

		// ServeHTTP 返回时响应已经写完，这里只做记录
		logger.Info("http",
			"method", r.Method, // GET / POST / PATCH
			"path", r.URL.Path, // 不含 query；路径足够定位是哪个端点
			"status", rec.status,
			"took", time.Since(started).Round(time.Microsecond).String(), // 耗时精确到微秒再取整
		)
	})
}

// withCORS 加跨域头，让"浏览器直连后端"这条路也能走通。
//
// 什么时候需要：
//   - 日常开发**不需要**——管理端走 vite 的 /api 代理，浏览器看到的都是同源请求
//   - 演示时想直接打开 dist 产物，或把页面放到另一个端口，就需要
//
// 放开 * 的前提是 v1 没有鉴权（题目没要求）。真部署必须换成白名单，
// 否则任何网站都能代替用户调这个接口。
//
// OPTIONS 分支是"预检请求"：浏览器要发非简单请求（比如带
// Content-Type: application/json 的 PATCH）时会先发 OPTIONS 问"允不允许"。
// 它没有业务语义，回 204 就行；不在这里拦掉的话，它会一路落到兜底 handler 变成 404。
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
