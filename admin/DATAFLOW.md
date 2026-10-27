# admin 数据流转核验手册

> 用途：让**你**（不是 AI）能独立确认"数据从哪来、到哪去、出错时去哪"。
> 本文只描述已经写进代码的行为。代码改了本文没改 = 本文是 bug。

---

## 0. 三十秒版本

这个 admin 只跟后端说三句话，一句都不多：

| # | 说什么 | 什么时候说 |
|---|---|---|
| 1 | `GET /api/health` | 页面加载时 + 每 10 秒一次（顶栏徽章） |
| 2 | `GET /api/inspections` | 点进来、点刷新、错误后点重试 |
| 3 | `PATCH /api/inspections/:id` | 你改某一条的状态时 |

**没有第四句话。** 抽屉（详情）、筛选、统计数字，全部不发请求——它们都是第 2 句话那次响应的派生物。

---

## 0.5 两个会让你困惑的现象（先看这里，省十分钟）

**① 开发模式下请求条数会翻倍。**
React 的 `StrictMode`（`src/main.tsx` 里）在开发模式会把 `useEffect` 故意跑两遍，用来暴露副作用问题。所以请求日志里可能看到两条一模一样的 `GET /api/inspections`。这是**开发模式专属**：`npm run build` 出来的产物只发一条。核验"应该发几次"时，以生产构建为准。

**② 后端没起时，看到的不是 Go 的 500，而是"连不上后端服务"。**
因为浏览器请求的是 vite dev server（同源 `/api`），由它转发给 Go。Go 不在时，vite 自己返回一个**空响应体的 500**。`api/client.ts` 把这种空 5xx 判定为网络错（`ECONNREFUSED` 同理），所以横幅写的是"连不上后端服务（代理没拿到任何响应）"——这比显示"HTTP 500"诚实。真后端返回 JSON 错误体时，走的是契约的四条路，不会混到这里。

---

## 1. 三个网络出口的准确位置

全站 `fetch` 只出现在一个文件里：`src/api/client.ts`。页面和 hook 里没有 `fetch`。

| 出口函数 | 方法 + 路径 | 谁调用 | 请求体 | 成功响应怎么用 |
|---|---|---|---|---|
| `getHealth()` | `GET /api/health` | `hooks/useHealth.ts` | 无 | 只喂顶栏徽章，不进列表 |
| `listInspections()` | `GET /api/inspections` | `hooks/useInspections.ts →` `reload()` | 无 | 逐条过 `parseInspection()` 校验后 `setItems()` |
| `patchStatus(id, next)` | `PATCH /api/inspections/:id` | `useInspections.ts →` `changeStatus()` | `{"status":"in_progress"}`（**只有这一个字段**） | 用响应里那**一整条**替换本地对应项 |

副作用之外的第二个出口：`src/api/log.ts` 里的 `pushLog()`。它不发网络请求，只把每次往返记进内存，显示在页面底部「请求日志」里。

---

## 2. 一次 GET 的完整链路（进页面时发生什么）

```
① 路由 / 命中 InspectionsPage                     src/App.tsx
② 渲染时 useInspections() 初始化                   hooks/useInspections.ts
   phase='loading', items=null
③ useEffect → reload()                            ← 唯一触发点
④ reload 里第一句：setPhase('loading')             → 页面显示 4 条骨架屏
⑤ listInspections()                               src/api/client.ts
   → fetch('GET /api/inspections')                ← 真正发出去
   → pushLog(...)                                 → 底部请求日志出现第一条
⑥ 收到 200 {"items":[…]} → items.map(parseInspection)
   每条都校验 id/title/priority/status/position/时间戳
   任何一条不合契约 → 抛 ApiError('bad_response')
⑦ setItems(list); setPhase('ready')
⑧ 页面从 items 派生三样东西，且只派生这三样：
   counts（四个统计数字） / visible（当前筛选下的行） / selected（抽屉里那条）
```

失败路径：⑥⑦ 换成

```
catch → setBanner(describeError(err)); setPhase('error')
      → 页面顶部横幅 + 「重试」按钮（点它 = 回到 ③）
      → items 保持 null（不显示假数据，也不显示空列表）
```

**关键点：`items === null` 和 `items === []` 是两件完全不同的事。**
前者是"没拿到"（错误态），后者是"后端说一条都没有"（空态）。代码里严格区分，见 `src/pages/InspectionsPage.tsx` 的四个条件分支。

---

## 3. 一次 PATCH 的完整链路（改状态时发生什么）

```
① 你在某行的下拉里选「处理中」
   StatusSelect.onChange → onPick('in_progress')   components/StatusSelect.tsx
② InspectionsPage 调 list.changeStatus(id, next)  pages/InspectionsPage.tsx
③ useInspections.changeStatus：
   setPendingId(id)                                → 该行下拉禁用 + 显示「提交中…」
   ⚠ 此刻该行状态文本仍然是旧值 —— 这就是"不做乐观更新"的证据
   setRowError(null)
④ patchStatus(id, next)                            src/api/client.ts
   → fetch('PATCH /api/inspections/insp_000123', {status:'in_progress'})
   → pushLog(...)                                  → 请求日志多一条，含请求体
⑤ 分三种结局：
   ✅ 200 → 用响应里那一整条替换本地：
           setItems(prev => prev.map(it => it.id === updated.id ? updated : it))
           setToast('已改为「处理中」')
           ↑ 注意：updatedAt 来自后端。前端从不自己造时间戳（A5）
   ⚠ 400/409 → describeError → scope='row'
           → setRowError({id, text}) → 红字挂在该行下面，本地数据一个字都没改
   ❌ 404/500/网络错/契约漂移 → scope='banner'
           → setBanner(...) → 顶部横幅 + 刷新按钮
⑥ finally: setPendingId(null)                      → 下拉恢复可点
```

**两个刻意的"不做"：**

1. **不做乐观更新**（不先把界面改掉再等后端）。理由：后端是唯一真源，界面必须是"某次响应"的投影。用 `MOCK_LATENCY_MS=1500` 能把这条差异看得一清二楚（见第 5 节实验 8）。
2. **前端不判断状态能不能改**。`resolved → open` 允许、其它回退 409，这个规则住在后端。前端要是自己拦一道，后端改规则时前端就开始骗人。

---

## 4. UI 状态机（全部状态都在两个 hook 里，没有第三处）

**列表**（`useInspections`）

| 状态 | 触发 | 页面表现 |
|---|---|---|
| `loading` + `items=null` | 首次进入 / 点刷新 | 4 条骨架屏 |
| `error` + `items=null` | GET 失败 | 顶部横幅 + 重试 |
| `ready` + `items=[]` | 后端返回空列表 | 空态文案 |
| `ready` + `items≠[]` | 正常 | 统计 + 筛选 + 行 |

**行**（同一组 state）

| 状态 | 变量 | 表现 |
|---|---|---|
| 正在提交 | `pendingId === 该行 id` | 下拉禁用 + 「提交中…」 |
| 行内错误 | `rowError.id === 该行 id` | 红字在该行下方，状态值不变 |
| 页面级错误 | `banner !== null` | 顶部横幅（记录可能已不存在 / 服务挂了） |

**health**（`useHealth`，与列表完全独立）

| 状态 | 含义 |
|---|---|
| `checking` | 正在请求 |
| `ok` | status/db 都正常 |
| `db_down` | 服务在、db 不是 ok（这时列表读得出来但写会失败） |
| `down` | 请求失败（后端没起 / 代理目标不对） |

---

## 5. 你可以亲手跑的 9 个核验（含"期望"和"不符合说明什么"）

先开两个终端：

```powershell
# 终端 A：mock 后端（Go 起来之后就不需要它了）
cd workspace/ar-device/admin
node tools/mock-api.mjs

# 终端 B：前端
npm run dev      # → http://localhost:5173
```

每条核验都打开页面底部的「请求日志」看，或者用 DevTools Network。**两者应该一致**；如果不一致，以请求日志为准（它是前端真正拿到的字节）。

| # | 你要做的事 | 应该看到 | 不符合说明 |
|---|---|---|---|
| 1 | 直接打开页面 | 请求日志第一条 `GET /api/inspections 200`；4 个统计数字 3/1/1/1；三行按创建时间倒序（B1 最新）；顶栏徽章「服务正常 · v0.1.0」 | 徽章有但列表空 → GET 没发出去或形状校验失败 |
| 2 | 点「刷新」 | 请求日志多一条 GET；「最后同步」时间更新 | 页面没变化但日志有一条 → 后端返回的内容和上次一样（正常） |
| 3 | 把 mock 进程 Ctrl+C 杀掉，再点刷新 | 骨架屏短暂出现 → 顶部横幅「连不上后端…」+ 重试；顶栏徽章变「未连接后端」 | 出现白屏或空态文案 → `items===null` 与 `[]` 被混用了，说明代码被改坏了 |
| 4 | 重新起 mock，点横幅上的「刷新」 | 回到正常列表 | — |
| 5 | 改一条待处理 → 处理中 | 请求日志多一条 `PATCH /api/inspections/insp_000123`，请求体 `{"status":"in_progress"}`，响应 200 里 `updatedAt` 已刷新；行状态变、右下 toast「已改为处理中」 | toast 出现但请求日志里 PATCH 是 409 → 前端乐观更新了（不该有） |
| 6 | `MOCK_FAIL=409 node tools/mock-api.mjs` 后改任意行 | 该行下方红字「已解决的问题不能重新处理」；**状态文本没变**；无 toast | 状态先变了又弹回来 → 被改成乐观更新的写法了 |
| 7 | `MOCK_FAIL=500` 起 mock 后改任意行 | 顶部横幅「服务异常，请重试」+ 刷新按钮 | 只在行内出现红字 → 错误分流（`describeError` 的 scope）被改了 |
| 8 | `MOCK_LATENCY_MS=1500` 起 mock 后改一条 | 1.5 秒内：下拉禁用 +「提交中…」+ **状态文本仍是旧值**；1.5 秒后：跳到新值 + toast | 立刻变了 → 乐观更新（同 6 的判据） |
| 9 | `MOCK_FAIL=bad_shape` 起 mock 后刷新 | 顶部横幅「记录的 status 不是契约值：pending_review…」；**列表不渲染烂数据** | 页面渲染出奇怪的行 → 形状校验被删了 |

再补三条 curl 级核验（绕开前端，直接验契约，和第 8 条对应）：

```powershell
# a. 幂等：同一状态再改一次，应 200（不是 409）
curl.exe -X PATCH http://127.0.0.1:8080/api/inspections/insp_000121 -H "Content-Type: application/json" -d "{\"status\":\"resolved\"}"

# b. 只许改 status：传 title 应 400 invalid_argument（契约 6.6-5）
curl.exe -X PATCH http://127.0.0.1:8080/api/inspections/insp_000123 -H "Content-Type: application/json" -d "{\"title\":\"x\"}"

# c. 回退：处理中 → 待处理 应 409 invalid_transition
curl.exe -X PATCH http://127.0.0.1:8080/api/inspections/insp_000122 -H "Content-Type: application/json" -d "{\"status\":\"open\"}"
```

---

## 6. 代码地图（哪一段负责哪条规则）

| 规则（架构设计 §五） | 落在哪 |
|---|---|
| A1 拉列表 + 三态齐全 | `hooks/useInspections.ts` + `components/States.tsx` |
| A2 渲染标题/优先级/状态，倒序 | `pages/InspectionsPage.tsx`（排序完全依赖后端返回顺序） |
| A3 等后端返回再更新界面 | `useInspections.changeStatus`（没有本地预改那一步） |
| A4 错误归一 | `api/client.ts → describeError()`（唯一一处把 code 翻成人话的地方） |
| A5 重试 + 不本地造数据 | 横幅/错误态的重试按钮 + `patchStatus` 用响应整条覆盖 |
| 契约字段名/形状 | `types/inspection.ts` + `api/client.ts` 的 `parseInspection` |

---

## 7. 契约里一个还没冻结的洞（需要和 server 线对齐）

契约 6.6 第 2 条只说了：

> 「允许 `resolved → open`；其它**回退**一律 409」

但**"前进"能不能跳级没说**——`open → resolved`（跳过"处理中"）到底允许还是 409？

- admin 这边不拦（`StatusSelect` 三个值都能选）。
- `tools/mock-api.mjs` 暂时按**允许跳级**实现。
- 真后端（Go）定了之后，如果它禁止跳级，admin 不需要改代码：用户会被 409 拦下，行内显示后端给的 message。**这就是"前端不判断规则"的好处。**
