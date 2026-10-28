# 接口契约（已冻结 2026-10-27）

> **这是三端唯一的事实来源。** Go struct tag / TS type / C# DTO 都必须和这里一致。
> 改契约 = 改这份文件 + 同时改三端 → 一次 commit 完成（规矩见第 6 节）。

---

## 1. 四个端点

"端点"＝后端对外开放的一个地址 + 一个动作。方法只有三种含义：`GET` 读、`POST` 新建、`PATCH` 改已有。

| # | 方法 + 路径 | 谁在用 | 干什么 | 请求体 | 成功响应 |
|---|---|---|---|---|---|
| 1 | `GET /api/health` | 手机端自检、管理端顶栏徽章、评审验证 | 报平安：服务活着吗、数据库通吗 | 无 | `200 {"status":"ok","db":"ok","version":"0.1.0"}` |
| 2 | `POST /api/inspections` | 手机端提交 | 新建一条；`id` / 时间 / 初始状态由后端生成 | `title`、`description?`、`priority`、`position` | `201` + 整条记录 |
| 3 | `GET /api/inspections` | 管理端打开页面 / 刷新 | 取全部问题，后端固定按 `createdAt` 倒序 | 无 | `200 {"items":[…]}`,  |
| 4 | `PATCH /api/inspections/:id` | 管理端改状态 | **只改 `status`** 一个字段 | `{"status":"in_progress"}` | `200` + 更新后整条 |

**列表为什么包一层 `{"items":[…]}`**：Unity 的 `JsonUtility` 无法反序列化顶层数组。包一层后三端都不需要自定义解析器。

**为什么没有别的端点**（这些是"想过但砍掉"的）：

| 想加的 | 为什么 v1 不要 |
|---|---|
| `GET /api/inspections/:id` | 没有详情页，加了没人调（详情用列表数据做抽屉） |
| `DELETE /api/inspections/:id` | 记录永不物理删除（BR-10），作废是状态不是删除 |
| `PUT /api/inspections/:id` | 全量替换会覆盖别人的改动，而我们只需要改一个字段 |
| `GET /api/inspections?status=&priority=` | 数据量小，前端过滤一次就够 |
| `POST /api/auth/login` | v1 不做权限 |
| `GET /api/stats` | 统计是加分项，v1 不做 |

---

## 2. 一条记录

```json
{
  "id": "insp_06GQW0PCPR6TVS8HRGHVRAKMJZ",
  "title": "B1 入口墙面破损",
  "description": "左侧墙体存在裂缝，长约 40cm",
  "priority": "high",
  "status": "open",
  "position": { "x": 0.42, "y": -1.30, "z": 1.26 },
  "createdAt": "2026-10-27T15:37:55Z",
  "updatedAt": "2026-10-27T15:37:55Z"
}
```

| 字段 | 类型 | 谁生成 | 约束 |
|---|---|---|---|
| `id` | string | server | 不透明字符串，前端只透传不解析。实际形态 `insp_` + 26 位 [ULID](https://github.com/ulid/spec) |
| `title` | string | 手机端 | trim 后 1–100 字，**必填** |
| `description` | string | 手机端 | 0–500 字，可空，缺省 `""` |
| `priority` | string（枚举） | 手机端 | `low` / `medium` / `high` |
| `status` | string（枚举） | **server** | `open` / `in_progress` / `resolved`；新建固定 `open` |
| `position` | object | 手机端 | `x`/`y`/`z` 三个有限浮点数，单位米，服务端收敛到 3 位小数 |
| `createdAt` | string | **server** | RFC3339，UTC |
| `updatedAt` | string | **server** | 同上，**每次真正改变状态时**刷新 |

**`position.y` 通常是负数**（原点在手机启动时的高度，地面在下方约 1.0–1.5 米）。这不是 bug。

---

## 3. 错误体

任何错误的响应体都只有这一种形状：

```json
{ "error": { "code": "invalid_argument", "message": "标题不能为空" } }
```

- `code` —— 给机器看的，四个固定值
- `message` —— 给人看的中文，可直接显示

| `code` | HTTP | 什么情况 | 三端各自怎么处理 |
|---|---|---|---|
| `invalid_argument` | 400 | 字段格式不对：标题空、优先级不认识、坐标不是有限数、PATCH 传了非 status 字段 | 手机端红字 / 管理端行内红字 |
| `not_found` | 404 | 改一条不存在的 id | 管理端「记录已不存在，请刷新」 |
| `invalid_transition` | 409 | 状态流转不被允许 | 管理端行内提示 + 控件回滚 |
| `internal` | 500 | 后端自己出错 | 统一「服务异常，请重试」 |

**四个 code 之外的 HTTP 状态码是框架行为，不走这个错误体**：`405 method_not_allowed`、`415 unsupported_media_type`、`413 payload_too_large` 由 Go 的 `ServeMux` / `http.Server` 直接返回**纯文本**。客户端解析失败时应归入「契约之外的响应」，不要当成业务错误。

---

## 4. 状态机（v1 已定）

| 从 → 到 | allowed | 说明 |
|---|---|---|
| `open → in_progress` | ✅ | 分诊、派人 |
| `in_progress → resolved` | ✅ | 干完了 |
| `open → resolved` | ✅ | **允许跳级**：现场自己好了 / 误报，一步关掉 |
| `resolved → open` | ✅ | **唯一允许的回退**：裂缝会复发（BR-07） |
| `in_progress → open` | ❌ 409 | 回退会抹掉「曾有进展」这个事实 |
| `resolved → in_progress` | ❌ 409 | 同上 |
| 任意 → 同一个值 | ✅ 200 | **幂等，且不刷新 `updatedAt`**（管理员手快点两次不该看到红字，也不该留下假痕迹） |

> 为什么允许向前跳级：跳的是**不携带信息**的状态，不是跳过了责任。真正负责任的字段（谁修的、凭什么算修好了）是 v1 的已知缺口，见第 7 节。

---

## 5. 校验顺序（决定"只报第一个错"）

**POST**，按此顺序，命中即返回：

1. 请求体是合法 JSON → 否则 400「请求体不是合法 JSON」
2. `title` trim 后非空 → 400「标题不能为空」
3. `title` ≤ 100 字 → 400「标题最多 100 字」
4. `description` ≤ 500 字 → 400「描述最多 500 字」
5. `priority` ∈ 三个枚举 → 400「优先级必须是 low、medium 或 high」
6. `position` 存在且三个轴都有 → 400「缺少位置坐标」
7. 三个轴都是有限数 → 400「坐标不是有效数字」

**PATCH**，顺序固定为 `400 → 404 → 409`：

1. body 合法、非空、只含 `status`、`status` 是合法枚举 → 400
2. 记录存在 → 404
3. 状态流转允许 / 或同状态幂等 → 409

顺序不能反：请求本身都不合法时，纠结记录在不在没有意义。

---

## 6. 冻结后的三条纪律

1. 改契约 = 改这份文件 + 同时改三端（Go struct tag / TS type / C# DTO）→ 一次 commit。
2. 改完立刻跑冒烟：`server/scripts/smoke.ps1`（PowerShell）或第 8 节的 curl。
3. 契约冻结后三端可完全分开开发，谁落后都不影响另外两端。

---

## 7. 已知缺口（如实声明，不是遗漏）

- **没有状态变更历史**（BR-08 要求留痕）。当前只能答"最后什么时候改的"，答不了"周三从什么改到什么"。补法：加一张 `inspection_events` 附表，**不动已冻结的四个端点**。
- **闭环只到"状态=已解决"**，缺"谁修的、凭什么算修好"（业务文档 §8 的第 2、3 条）。
- **没有截图**：位置是快照、换人不复现（BR-05），截图是唯一便宜的补丁。契约已预留（见第 9 节）。
- **没有迁移机制**：建表用的是 `CREATE TABLE IF NOT EXISTS`，只在启动时跑一次，"表已存在"就整条跳过。所以给表加字段（截图 `screenshot_url`、历史表 `inspection_events`）时，**已有数据库不会自动获得新列**，程序会带着旧表跑，然后在查询时报 `no such column`。当前处理方式：删库重建（`server/data/inspections.db*` 全删）。真要演进，需要在 `store.Open` 里按版本号补几条 `ALTER TABLE`，或引入迁移工具。
- 没有鉴权、没有分页、没有作废状态。

---

## 8. 冒烟命令（curl，可直接复制）

```powershell
# 1 健康
curl.exe http://127.0.0.1:8080/api/health

# 2 新建（注意 position.y 为负）
curl.exe -X POST http://127.0.0.1:8080/api/inspections -H "Content-Type: application/json" --data-binary "@create.json"

# 3 列表
curl.exe http://127.0.0.1:8080/api/inspections

# 4 改状态（把 <id> 换成上一步返回的 id）
curl.exe -X PATCH http://127.0.0.1:8080/api/inspections/<id> -H "Content-Type: application/json" -d "{\"status\":\"in_progress\"}"

# 5 校验失败的样子（应看到 400 + invalid_argument）
curl.exe -X POST http://127.0.0.1:8080/api/inspections -H "Content-Type: application/json" -d "{\"title\":\"\"}"
```

`create.json` 必须是 **UTF-8 无 BOM**，否则中文会坏：

```json
{"title":"B1 入口墙面破损","description":"左侧墙体存在裂缝","priority":"high","position":{"x":0.42,"y":-1.30,"z":1.26}}
```

> PowerShell 5.1 的 `Invoke-RestMethod` 传 string body 时按 ASCII 编码，中文变 `?`——测中文接口必须用 `curl.exe` + UTF-8 文件，或用 `[System.Text.Encoding]::UTF8.GetBytes()`。`scripts/smoke.ps1` 就是这么干的。

---

## 9. 预留扩展：现场截图（v1 不实现）

将来加的时候不破坏已冻结的四个端点：

- 新增第 5 个端点：`POST /api/inspections/:id/screenshot`，`multipart/form-data`，字段名 `file`，限 PNG/JPEG、≤2 MB
- 成功：`201 {"screenshotUrl":"/uploads/xxx.jpg"}`
- 记录结构新增**可空**字段 `screenshotUrl`（新记录为 `null`，老数据不受影响）
- **截图失败不阻塞主流程**：记录已经创建成功，截图只是补充
