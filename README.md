# AR 现场巡检问题标注

巡检员用手机在真实空间里放置标记并上报问题，管理员在网页上查看并更新处理状态。

三端最小闭环：**Unity(AR Foundation) 手机端 → Go 后端 → React 管理端**

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