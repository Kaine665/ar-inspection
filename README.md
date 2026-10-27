# AR 现场巡检问题标注

巡检员用手机在真实空间里放置标记并上报问题，管理员在网页上查看并更新处理状态。

三端最小闭环：**Unity(AR Foundation) 手机端 → Go 后端 → React 管理端**

## 三方分工

三条线**并行开发，互不阻塞**。唯一耦合点是 `docs/api.md`（契约，已冻结），三方都只按它写代码，不互相读对方的实现。

| 线 | 目录 | 负责内容 | 起点状态 | 现行卡点 |
|---|---|---|---|---|
| server | `server/` | 四个端点 + SQLite 持久化 + 冒烟脚本 | ✅ v1 已实现（34 项契约冒烟 + 13 项单测全绿） | 无。Go 1.27.3 在 `C:\Users\17130\dev\go`，未进系统 PATH（启动命令见 `server/README.md`） |
| admin | `admin/` | React+TS 列表页 + 三态修改 | ✅ v1 已实现（列表 + 详情抽屉 + 契约页） | 无。自带 mock 后端，可独立跑通，见 `admin/DATAFLOW.md` |
| ar-client | `ar-client/` | Unity AR Foundation 放置标记 + 上报 | 空（只有 Unity 用 .gitignore） | Unity Hub 缺 Android Build Support + SDK/NDK |

**协作约定**

- 每条线只动自己的目录；`docs/` 和根目录文件改动要打招呼，避免三方同时改同一个文件产生冲突。
- 提交粒度按主题拆（不要一锅端），提交信息写清是哪条线。
- 分支建议：`server` / `admin` / `ar-client` 各一条长期分支，`main` 由整合方合并。当前都在 `main` 上，未建分支。

## 目录

| 目录 | 内容 |
|---|---|
| `ar-client/` | Unity 工程（AR Foundation 放置标记 + 上报表单）。用 Unity Hub 打开这个目录 |
| `server/` | Go 后端：REST API + SQLite 持久化 |
| `admin/` | React + TypeScript 管理端：问题列表 + 状态修改 |
| `docs/` | 接口文档与截图 |

## 启动

```powershell
# 后端（纯 Go，不需要 cgo / gcc）
cd server
$env:PATH = "C:\Users\17130\dev\go\bin;$env:PATH"; $env:GOCACHE = "$env:TEMP\gocache"
go run ./cmd/api                       # → :8080，库在 data/inspections.db

# 管理端（vite 已把 /api 代理到 127.0.0.1:8080，接真后端不用改任何配置）
cd admin; npm install; npm run dev     # → http://localhost:5173

# 手机端
# Unity Hub 打开 ar-client/ → 真机运行（需先装 Android Build Support + SDK/NDK）
```

没有后端时也能单独跑管理端：`cd admin; node tools/mock-api.mjs`（照契约实现的临时后端，支持故障注入）。

契约冒烟（改了后端或契约之后必跑）：

```powershell
cd server
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke.ps1
```

## 已完成 / 未完成 / 已知问题

**已完成**：后端四端点 + SQLite 持久化 + 统一错误体 + 优雅退出（34 项冒烟、13 项单测）；管理端列表/三态/改状态/详情抽屉/契约页（含请求日志核验面板）；契约文档落在 `docs/api.md`。

**未完成 / 已知缺口**（详见 `docs/api.md` 第 7 节）：Unity 手机端（环境未就绪）；状态变更历史表（BR-08 要求留痕）；处理人与验收证据；截图上传（契约已预留端点）；鉴权、分页、作废状态、离线队列。

## AI 使用记录

见仓库根目录 `AI_LOG.md`。