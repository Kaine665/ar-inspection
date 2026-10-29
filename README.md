# AR 现场巡检问题标注

巡检员用手机在真实空间里放置 AR 标记并上报问题，管理员在网页上查看并更新处理状态。

三端最小闭环：**Unity(AR Foundation) 手机端 → Go 后端 → React 管理端**

---

## 当前状态


| 端   | 目录           | 状态            | 技术栈                                                             | 现在能做什么                                                                           |
| --- | ------------ | ------------- | --------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| 手机端 | `ar-client/` | ✅ 真机跑通（L1–L4） | Unity 2022.3.62f3 · AR Foundation/ARCore 5.1.6 · IL2CPP · ARM64 | 检测水平平面 → 点一下放橙色方块（锚定，转身回来不飘）→ 填标题/描述/优先级 → 提交到后端 → 方块变绿表示已入库；顶部常驻后端连接状态与「重连后端」按钮 |
| 后端  | `server/`    | ✅ v1          | Go 1.26 · `modernc.org/sqlite` v1.61.0（纯 Go，无 cgo）              | 四个端点、SQLite 持久化、统一错误体、请求 id、panic 兜底、优雅退出；37 项契约冒烟 + 24 项单测全绿                    |
| 管理端 | `admin/`     | ✅ v1          | React 18.3 · TypeScript 5.6 · Vite 5.4                          | 列表 + 三态过滤 + 详情抽屉 + 状态修改（下拉）+ 后端健康徽标 + 契约页 + 请求日志核验面板                             |


契约是三条线唯一的耦合点，已冻结在 `docs/api.md`（四个端点、记录结构、错误体、状态机、校验顺序、已知缺口、curl 冒烟命令）。开发版本一对应 tag `v1-dev`（只含前后端，不含 Unity）。

---



## 接口

后端就四个端点，手机端与网页端读写同一份 SQLite 数据。完整定义（字段约束、校验顺序、状态机、错误码）在 `docs/api.md`。


| 方法 + 路径                      | 干什么                    | 请求体                                                | 成功响应                                              |
| ---------------------------- | ---------------------- | -------------------------------------------------- | ------------------------------------------------- |
| `GET /api/health`            | 服务与数据库是否正常             | 无                                                  | `200 {"status":"ok","db":"ok","version":"0.1.0"}` |
| `POST /api/inspections`      | 新建一条问题                 | `title`、`description`、`priority`、`position{x,y,z}` | `201` + 整条记录，`id` / 时间 / `status=open` 由后端生成      |
| `GET /api/inspections`       | 取全部问题，按 `createdAt` 倒序 | 无                                                  | `200 {"items":[…]}`                               |
| `PATCH /api/inspections/:id` | 只改 `status` 一个字段       | `{"status":"in_progress"}`                         | `200` + 更新后整条                                     |


一条记录：

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

`position` 是 AR 会话坐标，单位米；`y` 常为负，因为原点是手机启动时的高度，地面在下方。`status` 取值 `open` / `in_progress` / `resolved`，`priority` 取值 `low` / `medium` / `high`。

任何错误都只有一种体：`{"error":{"code":"invalid_argument","message":"标题不能为空"}}`，`code` 五个固定值 `invalid_argument`(400) / `not_found`(404) / `method_not_allowed`(405) / `invalid_transition`(409) / `internal`(500)。405 表示路径对、方法不对（如 `DELETE /api/inspections`），响应会带 `Allow` 头说明该路径认哪些动词。

---



## 界面

管理端三个页面（截图里的数据由 `server/scripts/seed.ps1` 造）：


| 问题列表 | 详情抽屉 |
|---|---|
| ![问题列表](docs/img/01-列表.png) | ![详情抽屉](docs/img/02-详情抽屉.png) |


「契约与状态」页把四个端点、五个错误码、以及每次请求的原始收发都摊在界面上，不用开 DevTools 也能核验数据是怎么流的：

![契约与请求日志](docs/img/03-契约与请求日志.png)

手机端界面在真机上看（见下方「手机端怎么配后端地址」）。

---

## 演示视频

[完整演示（3 分 06 秒，1080p，70 MB）](docs/video/01-完整演示.mp4) —— 一条连贯录屏，手机端与电脑端都在里面。播放器跳到对应位置即可：

| 时间 | 画面 |
|---|---|
| 0:00–0:40 | 项目结构、README 与 AI_LOG |
| 0:40–1:10 | 真机对地，平面网格出现 |
| 1:15–2:00 | 点一下放标记；HUD 显示 `SessionTracking / Planes: 1 / 已放置: 1` 与「后端 已连接 v0.1.0」 |
| 2:00–2:20 | 手机上填标题「地板漏液」、选优先级、提交 |
| 2:20–2:40 | 电脑刷新列表看到这条，点开详情抽屉（优先级 / 坐标 / 时间） |
| 2:40–2:55 | 抽屉里改状态 → 已解决，右下角「已改为「已解决」」 |
| 2:55–3:06 | 终端里服务端优雅退出（`收到退出信号，正在优雅关闭` → `已退出`）→ 重新 `go run ./cmd/api`（`server 已启动 env=dev … cors=*`）→ 列表刷新回来，数据一条不少 |

> 作业原文写的是「两段 3–5 分钟（手机和电脑）」。这里是一条完整流程，上表把手机段（0:40–2:20）和电脑段（2:20–3:06）标了出来——闭环比拼接更能说明问题。

## 交付内容

| 作业要求 | 在仓库里的位置 |
|---|---|
| Unity / React / Go 完整源码 | `ar-client/` · `admin/` · `server/`，保留完整 Git 历史 |
| README（版本、启动、真机配后端、已完成/未完成/已知问题） | 本文件 |
| AI_LOG.md（prompt 原文 + 结果 + 验证） | [AI_LOG.md](AI_LOG.md) |
| 演示视频 | [docs/video/01-完整演示.mp4](docs/video/01-完整演示.mp4) |
| 已完成 / 未完成 / 已知问题 | [docs/status.md](docs/status.md) |
| 设计与过程材料 | [docs/README.md](docs/README.md) |

---

## 快速开始



### 1. 后端

```powershell
cd server
go run ./cmd/api            # → http://127.0.0.1:8080，库在 server/data/inspections.db
```

验一下：`curl http://127.0.0.1:8080/api/health` → `{"status":"ok","db":"ok","version":"0.1.0"}`

### 2. 管理端

```powershell
cd admin
npm install
npm run dev                 # → http://localhost:5173（vite 已把 /api 代理到 127.0.0.1:8080）
```

没有后端也能单独跑：`npm run mock`（照契约实现的临时后端，支持 `MOCK_FAIL=409/404/500/bad_shape/nonjson`、`MOCK_LATENCY_MS` 故障注入）。

### 3. 手机端

前置：Unity Hub 里 **2022.3.62f3**，带 Android Build Support（SDK/NDK/JDK 在 Unity 的 External Tools 里指向本机安装目录）；手机开 USB 调试，机身是 arm64。

前置还有一条：**`adb` 要在 PATH 里**。它属于 Android SDK 的 platform-tools，装 SDK 时不一定自动加进 PATH；报「无法将 adb 项识别为…」就是这个原因。两种解法选一个：

```powershell
# ① 只对当前窗口生效（关掉窗口就失效）
$env:PATH += ";<Android SDK>\platform-tools"

# ② 或一直用全路径（最不容易出错）
& "<Android SDK>\platform-tools\adb.exe" devices
```

`<Android SDK>` 换成自己的 SDK 目录，就是 Unity → Preferences → External Tools 里填的那个。

```powershell
# ① 先把手机的 8080 转发到电脑（必须做，否则手机连不上后端）
adb reverse tcp:8080 tcp:8080

# ② 装机（APK 在本地构建产物目录，不进仓库）
adb install -r ar-client/Builds/app.apk

# ③ 起后端（见上面第 1 步），保持它开着
```
Unity Hub 打开 `ar-client/` 目录 → `Assets/Scenes/Main.unity` → Build And Run 也可以。场景装配与打包有脚本：`Assets/Editor/ProjectSetup.cs`（一次性搭场景，不靠手点）、`Assets/Editor/BuildScript.cs`（命令行打包）。

**手机上怎么操作**：对着地面缓慢左右晃 → 出现平面网格 → 点一下放方块 → 弹出表单填标题（必填）/描述/优先级 → 提交 → 顶部提示"已上报 insp_xxx"，方块变绿。

---

## 手机端怎么配后端地址

手机不是电脑，`localhost` 指的是手机自己。两种接法，默认走第一种：

**① adb reverse（默认，推荐）** —— 不依赖 Wi-Fi，也不用改 IP。
`ar-client/Assets/Scripts/AppConfig.cs` 里 DEV 环境的 `DevBaseUrl = "http://127.0.0.1:8080"`，配合 `adb reverse tcp:8080 tcp:8080`，手机的 127.0.0.1:8080 就是电脑的 8080。

⚠️ 这个转发会在 **USB 断开重连、adb 服务重启、手机锁屏断开调试、重新 install** 之后被清空，症状就是 App 里报"连不上后端"。重新执行一次 `adb reverse tcp:8080 tcp:8080` 即可，代码不用动。

App 顶部第一行常驻后端状态：`后端 已连接 v0.1.0 · 12:03:45` 或 `后端 未连接 · 点「重连后端」重试`。启动时查一次、每 20 秒兜底重查、每次提交后立刻刷新；转发断了不用重启 App，补一条 `adb reverse` 再点按钮即可。

**② 局域网直连** —— 想脱离 USB 时用。
在 App 里执行一次 `AppConfig.SetDevBaseUrl("http://<电脑局域网IP>:8080")`（不用重新打包，重启 App 恢复默认）；或改 `AppConfig.cs` 的 `DevBaseUrl` 后重新打包。手机与电脑同一网段，电脑防火墙放行 8080。

失败提示分两种，来自契约错误体：后端讲理由的（如 400 `invalid_argument`）直接显示它的 message；压根没连上的显示"连不上后端服务"。表单在失败时**不清空已填内容**，可以直接重试。点「取消」= 这次上报作废，方块连同锚点一起消失，不会留下孤儿标记；只有提交成功才会留下绿色标记。

---

## 配置（环境变量）

三端各有一处配置入口，代码里不写死地址、超时、工具链路径。

**后端 `server/` —— 环境变量（进程启动前设）**

| 变量 | dev 默认 | prod 默认 | 说明 |
|---|---|---|---|
| `APP_ENV` | `dev` | — | `dev` / `prod`；写成别的直接启动失败，不带可疑配置跑 |
| `ADDR` | `:8080` | `:8080` | 监听地址 |
| `DB_PATH` | `data/inspections.db` | 同左（会提示它依赖启动目录） | SQLite 文件路径 |
| `LOG_LEVEL` | `debug` | `info` | `debug` / `info` / `warn` / `error` |
| `CORS_ORIGINS` | `*`（全放开） | 空 = 不发跨域头 | 生产禁止 `*`（会拒绝启动）；需要跨域时写具体来源，逗号分隔 |

```powershell
# 开发
cd server; go run ./cmd/api                    # 等价于 APP_ENV=dev

# 生产：跨域默认关（管理端与后端同源部署最省事）；分域名就列白名单
$env:APP_ENV="prod"
$env:DB_PATH="D:\data\inspections.db"
$env:CORS_ORIGINS="https://admin.example.com"
go run ./cmd/api
```

启动日志会把 `env / addr / db / version / cors / log` 全打出来——出问题第一件事就是确认"跑的是哪份配置"。

**管理端 `admin/` —— `.env` 文件（vite 按命令自动选）**

| 文件 / 变量 | 作用 |
|---|---|
| `.env.development`（`npm run dev`） | 开发配置，走 vite 代理 |
| `.env.production`（`npm run build`） | 生产配置，默认同源部署 |
| `VITE_API_BASE` | 空 = 同源 `/api`；只有前后端分域名才填完整地址 |
| `VITE_API_TIMEOUT_MS` | 单次请求超时：dev 8000、prod 12000 |
| `ADMIN_PROXY_TARGET` | 仅 dev：vite 把 `/api` 代理到哪（默认 127.0.0.1:8080） |
| `ADMIN_PORT` | 仅 dev：端口，默认 5173 |

配置在 `src/config.ts` 收口，页面和 api 层不直接读 `import.meta.env`。顶栏右上角常驻一个 `DEV` / `PROD` 标签，防止"以为在测生产"。

**手机端 `ar-client/` —— 编译宏 + 系统环境变量**

| 配置 | 怎么改 | 说明 |
|---|---|---|
| 后端地址 / 环境 | Unity 菜单 `AR/2. Backend = DEV` / `AR/3. Backend = PROD` | 切换编译宏 `AR_PROD`：DEV 连 `127.0.0.1:8080`（配 `adb reverse`），PROD 连 `AppConfig` 里的 HTTPS 地址。生产包里连 dev 地址的字符串都不会被打进去 |
| 运行期改地址（仅 DEV） | `AppConfig.SetDevBaseUrl("http://192.168.1.20:8080")` | 走局域网直连时不用重新打包；重启恢复默认 |
| Android 工具链路径 | 系统环境变量 `ANDROID_SDK_ROOT` / `ANDROID_NDK_ROOT` / `JAVA_HOME` | 读不到才回退到本机默认目录（`ProjectSetup.cs`） |
| 超时 / 轮询间隔 | `Assets/Scripts/AppConfig.cs` | 健康检查 4s、提交 10s、20s 兜底轮询，集中一处 |

---

## 测试与验证

| 项 | 命令 | 现状 |
|---|---|---|
| 服务端契约冒烟（37 项） | `cd server; powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke.ps1` | 全绿 |
| 服务端单测（24 个测试函数） | `cd server; go test ./...` | 全绿 |
| 管理端类型检查 + 构建 | `cd admin; npm run build` | 通过（gzip 约 62 kB） |
| 管理端真点 | 浏览器 + 请求日志面板 | 改状态 → PATCH → 界面与统计同步；`MOCK_FAIL` 各分支都亲手触发过 |
| 手机端 | 真机（小米 24129PN74C / arm64） | L1 平面 → L2 放置不飘 → L3 上报入库 → L4 网页可见并可改，逐环点过 |

改了后端或契约之后，冒烟脚本必跑。

---

## 工程结构

```

ar-device/
├─ docs/api.md          冻结的接口契约（唯一耦合点）
├─ server/              Go：cmd/api 装配 + internal/{model,store,api} 四层单向依赖
├─ admin/               React+TS：src/{api,components,hooks,pages,types}，DATAFLOW.md 记录数据流
└─ ar-client/           Unity：Assets/Scripts/{Api,ARPlacement,SubmitForm,ARHUD}.cs + Assets/Editor/{ProjectSetup,BuildScript}.cs

```

三条线并行开发、互不阻塞，只按 `docs/api.md` 写代码。提交粒度按主题拆，提交信息标明是哪条线。

## 设计与过程文档

设计与过程材料收在 `docs/`，入口是 **[docs/README.md](docs/README.md)**：

- `docs/design/` —— 业务规则、架构设计、可靠性与可维护性的验收条目、断言与核验（5 条链路 / 24 个检查点）
- `docs/process/` —— 方法记录、迭代计划、技术学习清单、开源借鉴清单、录屏脚本、提示词脉络

只想判断"是否真做过 AR"，直接看 `docs/design/04-断言与核验.md` 里链路一的逻辑表，以及其中"跟踪不可靠时禁止放置"的判定口径。

### 提示词脉络

159 条 Prompt 背后的 5 段转向与三次主动缩小目标。文字版见 [docs/process/06-提示词脉络.md](docs/process/06-提示词脉络.md)。

![提示词脉络：搭闭环 → 交付化 → 找尺子 → 造方法 → 按范围验收](docs/img/04-提示词脉络.png)

## AI 使用记录

见仓库根目录 `AI_LOG.md`：全部 prompt 原文，按时间正序，每条含目标、结果（采用 / 修改后采用 / 未采用）与验证方式，开头有索引表。

