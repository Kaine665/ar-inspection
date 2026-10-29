// 唯一可执行入口。这里只做装配，不写业务：
// 读配置 → 开库 → 建路由 → 起 HTTP → 等信号 → 优雅退出。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ar-inspection/server/internal/api"
	"ar-inspection/server/internal/config"
	"ar-inspection/server/internal/store"
)

// version 由 -ldflags "-X main.version=1.2.3" 覆盖，默认给个能看的值，
// 它会出现在 GET /api/health 的响应里，方便确认「部署的到底是哪个版本」。
var version = "0.1.0"

func main() {
	// 配置先于一切。配置错了就别启动——带着可疑配置跑起来，比不跑更难查。
	cfg, err := config.Load()
	if err != nil {
		slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("启动失败：配置有误", "err", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		logger.Error("启动失败：数据库打不开", "db", cfg.DBPath, "err", err)
		os.Exit(1)
	}
	defer st.Close()

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: api.New(st, logger, version, api.WithCORS(cfg.CORSOrigin)).Handler(),
		// 四个超时都显式给：没有它们，一个慢客户端就能一直占着连接
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 启动日志把这些值全打出来：出问题时第一件事就是确认"跑的是哪份配置"
	logger.Info("server 已启动",
		"env", cfg.Env,
		"addr", cfg.Addr,
		"db", cfg.DBPath,
		"version", version,
		"cors", describeCORS(cfg.CORSOrigin),
		"log", cfg.LogLevel.String(),
	)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Ctrl+C 或 SIGTERM 都能优雅退出：先把在飞的请求放完（最多 5 秒），再关库。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		logger.Error("监听失败", "err", err)
		os.Exit(1)
	case <-ctx.Done():
		logger.Info("收到退出信号，正在优雅关闭")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("关闭时出错", "err", err)
	}
	logger.Info("已退出")
}

// describeCORS 把"空字符串"翻译成人能读的说法，免得看日志时以为是配置漏了。
func describeCORS(origin string) string {
	if origin == "" {
		return "off（同源部署，不发跨域头）"
	}
	return origin
}
