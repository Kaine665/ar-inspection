# AI_LOG

作业交付要求中包含「AI 使用记录」。三条线各自往这里追加，按时间倒序（最新在最上面）。

## 记录格式

```
### YYYY-MM-DD HH:mm · <线：server / admin / ar-client>

**Prompt 原文**
> 原样粘贴，不要润色

**产出**
- 改了哪些文件、做了什么决定
- 与 AI 建议不一致的地方 + 为什么

**踩坑**
- 报错原文 / 怎么绕过的
```

---

## 记录

### 2026-10-27 23:45 · server · 第一版实现

**Prompt 原文**
> 我觉得差不多可以了。那再看后端吧。

> 一般生产环境中，跳过处理中，允许吗？

> 那咱们就允许，当然，在这之前，我得请你告诉我server的文件夹结构为何这样设计，我不太明白。我没用过go

> go.mod和main.go的差别是啥？

> 懂了，开始执行吧

**这一轮定下的规则**

- `open → resolved` **允许跳级**（现场自己好了 / 误报，一步关掉）；`in_progress → open` 仍 409。
- 同一状态重复 PATCH = 200 幂等，**且不刷新 `updatedAt`**（真幂等，不留假痕迹）。
- 校验顺序写死：POST 七步、PATCH `400 → 404 → 409`，命中即返回（契约要求"只报第一个"）。

**产出**

- `docs/api.md`：把冻结契约从作业文档搬进仓库，并补上本轮拍板的两条、id 形态、405/415/413 属框架纯文本的声明、已知缺口清单。
- `server/`：`cmd/api` 装配 + `internal/{model,store,api}` 三层单向依赖，`modernc.org/sqlite` 纯 Go 无 cgo。34 项契约冒烟 + 13 项 model 单测全绿。
- 端到端验证：管理端（React）→ vite 代理 → Go → SQLite，点一下下拉，`PATCH` 带真实 ULID、`updatedAt` 刷新、界面同步；**admin 一行代码都没改**。

**实现取舍**

- `db.SetMaxOpenConns(1)`：SQLite 只允许一个写者，串行排队比让请求互相撞 `database is locked` 省心。
- `ORDER BY created_at DESC, id DESC`：`createdAt` 只到秒，同一秒建的多条需要 id 做 tie-break，否则顺序不确定。
- 未知路径也返回契约错误体；只有 405/415/413 是框架默认纯文本（已在 docs 声明，客户端归入 `bad_response`）。
- 幂等分支不执行 UPDATE：语义上"什么都没发生"，就不该动时间戳。

**踩坑**

- **无 BOM 的 .ps1 在 PowerShell 5.1 里按 ANSI 解析** —— 冒烟脚本里所有中文变乱码、字符串错位、断言参数全串位（`Check` 报 "Cannot convert value System.String to Boolean"）。这是 2026-10-25 记过的坑，又踩了一次。修法：脚本加 UTF-8 BOM（`EF BB BF`），断言函数参数改弱类型。
- 本机 `%LocalAppData%` 缺失，`GOCACHE` 必须显式指向 `$env:TEMP\gocache`，否则 `go run` 报缓存目录不可用。
- 自写 ULID 的 base32 补齐写错过一次（`out[len(out)-width:]` 在长度不足时 panic），改成左补零；单测用 500 次同毫秒生成钉住唯一性。

---

### 2026-10-27 22:52 · admin · 第一版实现（含工程初始化）

**Prompt 原文**
> 先把admin建起来。不过你这一轮只设计项目有哪几个页面，有没有共用结构。每个页面大概长啥样。让我看了，再施工。

> 嗯，我认可你的四个选择。
>
> 我用自己的话讲一下你的设计。一个是检测后端服务是否正常运行。另外就是get数据库中存储的问题数据，以及进行处理，对吧。
>
> 这里的话，因为前端比较简单。所以我觉得，具体的前端代码实现我可以交给你，但是数据流转，我必须自己核验。

> 你弄好以后。做两次git提交。
>
> 一个是项目初始化。也就是，项目骨架建立
> 第二个是admin第一版提交

**设计阶段的决定（被用户认可）**

- 页面数定为 **1 个主页面 + 1 个详情抽屉 + 1 个契约页**。不做"列表 + 详情"两个页面：契约冻结后没有 `GET /api/inspections/:id`，详情路由拿不到数据，所以做成列表页内的抽屉，数据直接取自列表那次响应。
- 留指标条（四个状态计数）、留三态过滤 tab（纯前端、不加端点）、状态控件用下拉而不是三段按钮、加 `/system` 契约页。

**产出**

- `68b92e7` 工程初始化：Vite + React + TS strict；`src/types` 逐字段翻译契约 6.2；`src/api/client.ts` 作为唯一网络出口（超时 / 错误归一 / 响应形状校验）；`src/hooks` 列表状态机；`tools/mock-api.mjs` 照契约实现的临时后端（带故障注入）。
- `1712e91` 第一版：AppShell + 列表页 + 抽屉 + 契约页 + 请求日志面板 + `DATAFLOW.md`。
- 为"数据流转可核验"专门做的三件事：① 底部请求日志面板，显示每次往返的原始请求体/响应体；② `DATAFLOW.md` 写清三个网络出口的准确位置与 GET/PATCH 全链路；③ mock 支持 `MOCK_FAIL=409/404/500/bad_shape/nonjson`、`MOCK_LATENCY_MS`，让每条错误分支都能被亲手触发。

**实现上的取舍（与"看起来更专业"的写法不一致的地方）**

- **不做乐观更新。** 点下拉后界面仍是旧状态，等 PATCH 返回才变。视觉上"慢一拍"，但后端是唯一真源，用 `MOCK_LATENCY_MS=1500` 能看见差异。
- **前端不判断状态能不能改。** `resolved → open` 允许、其它回退 409，规则留在后端；前端只负责显示后端给的 message。
- **空 5xx 当成网络错。** vite 代理连不上 Go 时返回空响应体的 500，直接按 HTTP 状态码显示"HTTP 500"会误导，所以在 `client.ts` 里把它归成 `network`，提示"连不上后端服务"。

**踩坑**

- `npm install` 后 esbuild 的 postinstall 未执行（npm 11 的 allowScripts 策略）；实测 `@esbuild/win32-x64/esbuild.exe` 存在，构建正常，未额外处理。
- **vite dev server 被编辑器的原子保存搞崩**：`Error: EBUSY: resource busy or locked, watch '...client.ts.26468.…tmpdir\client.ts.tmp'`。解法是在 `vite.config.ts` 里 `server.watch.ignored` 忽略 `*.tmpdir` 与 `*.tmp`。
- 首次 `tsc` 报 `TS6196: 'ApiErrorBody' is declared but never used`，改为把它用在 `extractError` 的返回类型上。

**验收方式（真点了一遍，不是只跑了构建）**

- `tsc --noEmit` 通过；`vite build` 通过（187.71 kB / gzip 62.17 kB）。
- headless Chrome + CDP 真点：改状态 → 发 `PATCH …/insp_000123`，body 只有 `{"status":"in_progress"}`，200 返回整条，`updatedAt` 刷新，行内状态与四个统计数字同步变化。
- `MOCK_FAIL=409` → 行内红字 + 控件回滚 + 无 toast；`MOCK_FAIL=500` → 顶部横幅；杀掉后端 → 「连不上后端服务（代理没拿到任何响应）」+ 重试，恢复后重试成功。

---

### 2026-10-27 22:24 · 全局 · 仓库初始化

**Prompt 原文**
> 我接下来要做三方分工。不过，好像还没建仓库。咱们直接建在mino工作区下的workspace吧。叫ar-device

**产出**
- 把既有骨架仓库（原 `mino/ar-inspection/`，含 1 个 commit）迁到 `mino/workspace/ar-device/`，git 历史保留
- README 补「三方分工」一节；新增本文件
