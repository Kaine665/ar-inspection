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
