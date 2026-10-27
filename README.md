# AR 现场巡检问题标注

巡检员用手机在真实空间里放置标记并上报问题，管理员在网页上查看并更新处理状态。

三端最小闭环：**Unity(AR Foundation) 手机端 → Go 后端 → React 管理端**

## 三方分工

三条线**并行开发，互不阻塞**。唯一耦合点是 `docs/api.md`（契约，已冻结），三方都只按它写代码，不互相读对方的实现。

| 线 | 目录 | 负责内容 | 起点状态 | 现行卡点 |
|---|---|---|---|---|
| server | `server/` | 四个端点 + SQLite 持久化 + 冒烟脚本 | 骨架（`cmd/api`、`internal/{api,model,store}` 空目录） | Go 1.27.3 已装在 `C:\Users\17130\dev\go`，未进系统 PATH |
| admin | `admin/` | React+TS 列表页 + 三态修改 | 空 | 无，可先用 mock 数据开工 |
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

## 启动（待补齐）

```powershell
# 后端
cd server; go run ./cmd/api

# 管理端
cd admin; npm install; npm run dev

# 手机端
# Unity Hub 打开 ar-client/ → 真机运行
```

## 已完成 / 未完成 / 已知问题

待补。

## AI 使用记录

见仓库根目录 `AI_LOG.md`。