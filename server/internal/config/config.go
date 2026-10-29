// Package config 是服务端配置的唯一出处：环境名、监听地址、库路径、日志级别、跨域白名单。
//
// 三条纪律：
//  1. 配置只从环境变量来，代码里不写"线上地址"。换部署只改启动参数，不改代码、不重编译。
//  2. 开发与生产的默认值分开给：APP_ENV=dev 时一切都往"方便"偏（跨域放开、日志详细），
//     APP_ENV=prod 时往"安全"偏（跨域默认关、允许更严的白名单）。
//  3. 配置错了就启动失败，不带着可疑配置跑起来——带着错配置运行比不运行更难查。
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Env 是运行环境。只有两档，够用且不必猜。
type Env string

const (
	Dev  Env = "dev"
	Prod Env = "prod"
)

// Config 是启动所需的全部外部输入。
type Config struct {
	Env        Env
	Addr       string
	DBPath     string
	LogLevel   slog.Level
	CORSOrigin string // "" = 不发跨域头（同源部署）；"*" = 全放开（仅 dev 允许）
}

// IsProd 供少数需要按环境分支的地方使用（当前没有，但别让调用方去比较字符串）。
func (c Config) IsProd() bool { return c.Env == Prod }

// Load 读环境变量并校验。任何一项不合法都返回 error，由 main 决定怎么退出。
func Load() (Config, error) {
	env := Env(strings.ToLower(getenv("APP_ENV", string(Dev))))
	switch env {
	case Dev, Prod:
	default:
		return Config{}, fmt.Errorf("APP_ENV=%q 不认识，只接受 dev 或 prod", env)
	}

	cfg := Config{
		Env:      env,
		Addr:     getenv("ADDR", ":8080"),
		DBPath:   getenv("DB_PATH", "data/inspections.db"),
		LogLevel: parseLevel(getenv("LOG_LEVEL", defaultLogLevel(env))),
	}

	// 跨域默认值按环境分：
	//   dev  = "*"：本地演示可能直接打开 dist 产物、或换个端口跑，放开最省事。
	//         前提是 dev 库只有自己造的测试数据。
	//   prod = ""：不同源就不开。推荐管理端与后端同源部署（/api 反向代理到本进程），
	//         这样根本不需要 CORS；真要分域名，必须显式列出白名单。
	cfg.CORSOrigin = strings.TrimSpace(getenv("CORS_ORIGINS", defaultCORS(env)))

	if env == Prod && cfg.CORSOrigin == "*" {
		return Config{}, fmt.Errorf("生产环境不允许 CORS_ORIGINS=*：任何网站都能代替用户调接口。请写具体来源（逗号分隔），或留空走同源")
	}
	if env == Prod && cfg.DBPath == "data/inspections.db" && os.Getenv("DB_PATH") == "" {
		// 不算错误（Docker 里挂载到同一路径很常见），但要说一句：
		// 生产用默认相对路径，意味着库跟着"启动时的工作目录"走，换个目录启动就是另一个空库。
		fmt.Fprintln(os.Stderr, "[config] 提醒：生产环境未设置 DB_PATH，将使用相对路径 data/inspections.db（依赖启动目录）")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func defaultLogLevel(env Env) string {
	if env == Prod {
		return "info"
	}
	return "debug" // dev 下把每条 SQL/中间件细节都打出来，排障靠它
}

func defaultCORS(env Env) string {
	if env == Prod {
		return "" // 生产默认不开跨域（同源部署）
	}
	return "*"
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
