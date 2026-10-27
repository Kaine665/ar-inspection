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
	"ar-inspection/server/internal/store"
)

// version 由 -ldflags "-X main.version=1.2.3" 覆盖，默认给个能看的值，
// 它会出现在 GET /api/health 的响应里，方便确认「部署的到底是哪个版本」。
var version = "0.1.0"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	addr := getenv("ADDR", ":8080")
	dbPath := getenv("DB_PATH", "data/inspections.db")

	st, err := store.Open(dbPath)
	if err != nil {
		logger.Error("启动失败：数据库打不开", "db", dbPath, "err", err)
		os.Exit(1)
	}
	defer st.Close()

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.New(st, logger, version).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Info("server 已启动", "addr", addr, "db", dbPath, "version", version)

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

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
