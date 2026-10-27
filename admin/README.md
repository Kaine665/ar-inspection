# admin — AR 现场巡检管理端

React 18 + TypeScript + Vite。**一个列表页 + 一个详情抽屉 + 一个契约页**，没有别的。

数据只有两条来源，全部走 `docs/api.md` 第六节冻结的契约：

| 动作 | 请求 |
|---|---|
| 打开页面 / 刷新 / 重试 | `GET /api/inspections` |
| 改某一条的状态 | `PATCH /api/inspections/:id`，body 只有 `{"status":"…"}` |
| 顶栏徽章（每 10 秒） | `GET /api/health` |

**数据是怎么流转的、怎么自己核验** → 看 [`DATAFLOW.md`](./DATAFLOW.md)。那份是给人核验用的，不是给机器看的。

## 启动

```powershell
npm install

# ① 没有 Go 后端时：用本目录自带的 mock（严格照契约实现，含错误分支）
node tools/mock-api.mjs          # 127.0.0.1:8080
npm run dev                      # http://localhost:5173

# ② Go 后端起来之后：什么都不用改，直接起 dev 就行（代理默认指向 127.0.0.1:8080）
npm run dev

# ③ 代理目标不是 8080，或要直连后端（需要后端自带 CORS）
$env:ADMIN_PROXY_TARGET='http://127.0.0.1:9090'; npm run dev
$env:VITE_API_BASE='http://127.0.0.1:8080'; npm run dev

# ④ 生产构建
npm run build                    # 产物在 dist/
```

mock 的故障注入（用来核验错误分支，默认全正常）：

```powershell
$env:MOCK_FAIL='409'            # PATCH 一律 409 invalid_transition
$env:MOCK_FAIL='404'            # PATCH 一律 404 not_found
$env:MOCK_FAIL='500'            # PATCH 一律 500 internal
$env:MOCK_FAIL='bad_shape'      # 列表返回 status='pending_review'，看前端形状校验
$env:MOCK_FAIL='nonjson'        # 返回纯文本，模拟契约漂移
$env:MOCK_LATENCY_MS='1500'     # 人为变慢，看"不做乐观更新"
node tools/mock-api.mjs
```

## 目录

```
src/
├── api/client.ts        全站唯一的网络出口（含超时、错误归一、响应形状校验）
├── api/log.ts           请求日志（核验用，可整份删掉不影响业务）
├── types/inspection.ts  契约 6.2 的逐字段翻译
├── hooks/useInspections.ts  列表状态机 + 改状态（数据流转都在这里）
├── hooks/useHealth.ts   顶栏徽章，与列表完全独立
├── components/          AppShell / 行内控件 / 抽屉 / 三态 / 请求日志
└── pages/               InspectionsPage（主）+ SystemPage（契约与状态）
tools/mock-api.mjs       admin 线自测用的临时后端（不是交付物）
```

## 已做 / 未做

**已做**

- 列表：加载骨架 / 空态 / 错误态（带重试）三态齐全，默认按后端给的顺序（createdAt 倒序）
- 改状态：等后端返回再更新界面；`200` 幂等 · `409` 行内红字并回滚 · `404`/`500` 顶部横幅 · 契约漂移显式报错
- 前端筛选（全部 / 待处理 / 处理中 / 已解决）与四个计数，纯前端计算，不额外请求
- 详情抽屉：描述全文、坐标（含"y 为负是地面"的说明）、创建/更新时间
- 契约页：health 实时值、四端点表、四错误码表、可直接复制的冒烟命令

**未做（如实说明，不是遗漏）**

- 登录与权限（题目未要求）
- 编辑标题 / 优先级 / 坐标：契约冻结点 5 规定 PATCH 只接受 `status`，传其它字段 400
- 删除记录：BR-10 记录永不物理删除，作废是状态而不是删除（v1 未做作废态）
- 分页 / 搜索 / 排序参数：契约不支持 query 参数；数据量到几百条时需要加
- 统计报表、截图上传（契约 6.7 已预留端点）
- 乐观更新：刻意不做，后端是唯一真源

## 已知问题

1. **开发模式下请求会翻倍。** React 的 StrictMode 会把 `useEffect` 跑两遍，所以请求日志里可能看到两条一样的 `GET`。`npm run build` 的产物里只有一条。这解释见 `DATAFLOW.md` 第 0.5 节。
2. **`open → resolved` 跳级是否允许，契约没冻结。** 前端不拦，交给后端判定；mock 暂按"允许"实现，Go 那边定了以 Go 为准。
3. **Windows 上编辑器原子保存会让 vite watcher 崩（EBUSY）。** 已在 `vite.config.ts` 里 ignore 掉 `*.tmpdir` / `*.tmp`，别删这几行。
4. 没有错误上报、没有持久化 UI 态（刷新即回默认筛选），这是有意的：admin 不拥有任何状态。
