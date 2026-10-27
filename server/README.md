# server — Go 后端

四个端点 + SQLite 持久化。纯 Go，**不需要 cgo / gcc / Visual Studio Build Tools**，`go run` 一条命令就能跑。

契约见 [`../docs/api.md`](../docs/api.md)，那是三端唯一的事实来源。

## 启动

```powershell
cd server
$env:PATH = "C:\Users\17130\dev\go\bin;$env:PATH"   # 本机 Go 装在这里，没进系统 PATH
$env:GOCACHE = "$env:TEMP\gocache"                   # 本机 %LocalAppData% 缺失，必须显式指定
go run ./cmd/api
```

看到这行就起来了：

```
level=INFO msg="server 已启动" addr=:8080 db=data/inspections.db version=0.1.0
```

环境变量（都有默认值）：

| 变量 | 默认 | 说明 |
|---|---|---|
| `ADDR` | `:8080` | 监听地址 |
| `DB_PATH` | `data/inspections.db` | SQLite 文件路径，目录不存在会自动创建 |

验证：

```powershell
curl.exe http://127.0.0.1:8080/api/health
# {"status":"ok","db":"ok","version":"0.1.0"}
```

**按 Ctrl+C 是优雅退出**：先把在飞的请求放完（最多 5 秒），再关库，不会留下半写的 SQLite。

## 测试

```powershell
go test ./...                                  # model 层：校验规则 + 状态机 + id 形状
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke.ps1   # 契约冒烟：34 项
```

`scripts/smoke.ps1` 覆盖四个端点、四个错误码、幂等、跳级、回退 409、未知路径兜底。

> 脚本文件必须带 UTF-8 BOM：PowerShell 5.1 会把无 BOM 的 .ps1 按 ANSI 解析，中文全变乱码、字符串错位。

## 目录

```
cmd/api/main.go              唯一入口：读配置 → 开库 → 起 HTTP → 等信号 → 优雅退出
internal/model/model.go      记录结构体 + 校验 + 状态机 + ULID。纯函数，不碰 IO
internal/model/…_test.go     表驱动单测
internal/store/store.go      SQL：建表 / Ping / List / Get / Insert / UpdateStatus
internal/api/router.go       路由 + 4 个 handler + 唯一的错误出口
internal/api/middleware.go   日志（每请求一行）+ CORS + 请求超时
scripts/smoke.ps1            契约冒烟
data/                        SQLite 运行时文件（不进 git）
```

依赖方向是单向的，编译器强制：`cmd/api → internal/api → internal/store → internal/model`。
箭头不能反过来，`model` 不 import 项目里任何其他包。

## 关键实现决定

| 决定 | 理由 |
|---|---|
| `modernc.org/sqlite`（纯 Go） | 评审 clone 下来 `go run` 就能跑，不用装 gcc / VS Build Tools |
| `id` = `insp_` + ULID | 自带 48 位毫秒时间戳、天然按时间有序；不依赖数据库自增 |
| `db.SetMaxOpenConns(1)` | SQLite 只允许一个写者。串行排队比让请求互相撞 `database is locked` 省心 |
| WAL + `busy_timeout=5000` | 演示时浏览器轮询和写入不会互相阻塞 |
| 表上 `CHECK (status IN …)` | 数据库层兜底：handler 校验被改坏时脏数据也进不来 |
| 校验顺序写死在代码注释里 | 契约要求"只返回第一个错误"，那顺序就必须确定 |
| 同状态 PATCH 不 UPDATE | 真幂等：不改数据、不刷新 `updatedAt`，不留假痕迹 |
| 内部错误只回「服务异常，请重试」 | SQL 细节进日志、不进响应体 |
| 未知路径也返回契约错误体 | 只有 405/415/413 是框架默认的纯文本（已在 docs/api.md 声明） |

## 已做 / 未做

**已做**：四个端点；`status/db/version` 健康检查；中文与 UTF-8；坐标 3 位小数收敛；重启后数据仍在；统一错误出口；每请求一行结构化日志；优雅退出；34 项冒烟 + 13 项单测。

**未做（写进 docs/api.md 第 7 节「已知缺口」）**：状态变更历史表（BR-08）、处理人与验收证据、截图上传（契约已预留）、鉴权、分页、作废状态、离线队列。
