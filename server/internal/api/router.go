// Package api 是 HTTP 这一层：决定「哪个路径 + 哪个方法」交给谁处理、把请求体翻译成业务对象、
// 把结果写回 JSON，并负责把错误翻译成状态码。它只做这三件事。
//
// 校验规则不在这里（在 model），SQL 不在这里（在 store）。
// 依赖方向：api → store → model，反过来编译不过。
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"ar-inspection/server/internal/model"
	"ar-inspection/server/internal/store"
)

// maxBodyBytes 是单个请求体的上限：64KB。
// 一条巡检记录撑死几百字节，所以额度极宽松；设上限是为了让超大请求在读取阶段
// 就被挡掉，而不是先整个吃进内存。
const maxBodyBytes = 64 << 10

// Server 是一次服务运行期间的全部依赖，只有三样：数据库、日志、版本号。
//
// 它自己不持有任何业务状态——状态全在 SQLite 里，所以进程重启不会丢东西。
// 这也解释了为什么这里没有"缓存"之类的字段。
type Server struct {
	store   *store.Store // 读写数据库的唯一通道
	log     *slog.Logger // 每请求一行日志
	version string       // 出现在 /api/health 里，用来确认部署的是哪个版本
}

// New 把依赖装进 Server。参数由 main.go 提供，这里不设默认值也不做校验。
func New(st *store.Store, logger *slog.Logger, version string) *Server {
	return &Server{store: st, log: logger, version: version}
}

// Handler 组装路由，返回给 http.Server 使用。
//
// 四条业务路由 + 一条兜底。请求进入后依次穿过 CORS、日志、路由三层。
//
// 兜底那条（"/"）不能省：没有它，未匹配的路径会拿到 Go 自带的纯文本 404，
// 形状不符合契约，客户端解析 JSON 会失败——那是最难查的一类问题。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)            // 服务与数据库是否健康
	mux.HandleFunc("GET /api/inspections", s.handleList)         // 取全部记录
	mux.HandleFunc("POST /api/inspections", s.handleCreate)      // 新建（唯一写入口）
	mux.HandleFunc("PATCH /api/inspections/{id}", s.handlePatch) // 只改状态
	mux.HandleFunc("/", s.handleNotFound)                        // 兜底：未知路径也返回契约错误体

	return withCORS(withLogging(s.log, mux))
}

// ---------------------------------------------------------------
// 健康检查
// ---------------------------------------------------------------

// healthResponse 的三个字段各有分工：status 表示"进程活着"，
// db 表示"库通不通"，version 表示"跑的是哪个版本"。
type healthResponse struct {
	Status  string `json:"status"`
	DB      string `json:"db"`
	Version string `json:"version"`
}

// handleHealth 回答一个问题：现在能不能干活。
//
// 它**永远返回 200**，库坏了也返回 200，只是 db 字段变成 "error"。
// 这样调用方能区分两种完全不同的故障：
//   - 请求本身失败 → 后端没起（网络层就没通），要去启动进程
//   - 200 但 db=error → 进程在、库挂了，这时读可能还行、写一定失败
//
// 它不碰任何业务数据，也不写库，只做一次 SELECT 1。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// 2 秒上限：库被锁死时宁可报 db=error，也不要让健康检查自己挂住
	ctx, cancel := contextWithTimeout(r, 2*time.Second)
	defer cancel()

	db := "ok"
	if err := s.store.Ping(ctx); err != nil {
		db = "error"
		// 不是致命错误（服务还活着），但值得留一笔
		s.log.Warn("health: 数据库 ping 失败", "err", err)
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", DB: db, Version: s.version})
}

// ---------------------------------------------------------------
// GET /api/inspections
// ---------------------------------------------------------------

// listResponse 把记录数组包在 items 里。
//
// 这层包装不是为了好看：Unity 的 JsonUtility 反序列化不了顶层数组，只能处理对象。
// 包一层之后三端都不需要写自定义解析器。
type listResponse struct {
	Items []model.Inspection `json:"items"`
}

// handleList 返回全部记录，供管理端打开页面时渲染。
//
// 排序不在这里做：store.List 的 SQL 里已经按 createdAt 倒序。
// 前端也不再排——"谁排"只能有一个答案，两处都排的话一旦不一致就说不清谁对。
//
// 空结果是合法的：{"items":[]} 表示"现在一条都没有"，这和"请求失败"是两码事，
// 管理端会把它们渲染成完全不同的界面（空态 vs 错误态）。
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()

	items, err := s.store.List(ctx)
	if err != nil {
		// 读列表失败只可能是库的问题，属于我们的错，回 500
		s.writeError(w, r, model.Internal("读取列表失败: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Items: items})
}

// ---------------------------------------------------------------
// POST /api/inspections
// ---------------------------------------------------------------

// createRequest 是手机端提交上来的原始内容，只有这四个字段有位置放。
// id、status、时间戳这些只有服务端能生成的字段这里压根没有对应字段，
// 所以客户端就算传了也会被忽略——这正是契约要求的（传了不报错，静默丢弃）。
type createRequest struct {
	Title       *string         `json:"title"`
	Description *string         `json:"description"`
	Priority    *string         `json:"priority"`
	Position    json.RawMessage `json:"position"`
}

// handleCreate 处理 POST /api/inspections —— 整个链路唯一的写入口，只有手机端调。
//
// 七步，顺序和契约第 5 节一致：
//
//	① 解 JSON → ② 翻译成 Draft → ③ 业务校验 → ④ 生成服务端字段
//	⑤ 落库 → ⑥ 出错就写错误体 → ⑦ 返回整条记录
//
// 六和七互斥：任何一步失败都当场 writeError + return，走不到最后一行。
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	// ① 把请求体解进 req。
	// 解不出来（不是合法 JSON / 空体 / 超过 64KB）就到此为止，返回 400。
	var req createRequest
	if err := decodeJSON(w, r, &req); err != nil {
		s.writeError(w, r, model.InvalidArgument("请求体不是合法 JSON"))
		return
	}

	// ② 翻译成 Draft：这一步只看"类型对不对、缺了什么"。
	// 比如坐标写成字符串而不是数字，在这里就会被拒；到这一步还不涉及业务规则。
	draft, perr := req.toDraft()
	if perr != nil {
		s.writeError(w, r, perr)
		return
	}

	// ③ 业务校验：标题非空且不超长、描述不超长、优先级是三个枚举之一、坐标三个有限数。
	// 命中第一条错就返回——契约要求"只报第一个"，所以判断顺序本身也是契约的一部分。
	if err := draft.Validate(); err != nil {
		s.writeError(w, r, err)
		return
	}

	// ④ 生成只有服务端能给的字段。
	// now 只取一次：createdAt 与 updatedAt 必须完全相等，
	// 分两次取时间会出现"创建时间比更新时间早几毫秒"这种假数据。
	now := model.Now()
	rec := model.Inspection{
		ID:          model.NewID(now),               // insp_ + ULID，自带毫秒时间戳，天然按时间有序
		Title:       draft.NormalizeTitle(),         // 存 trim 后的标题，避免"标题全是空格"这种脏数据
		Description: draft.Description,              // 原样保留，可空
		Priority:    model.Priority(draft.Priority), // 巡检员初判的响应承诺：高=当天、中=本周、低=排期
		Status:      model.StatusOpen,               // 新记录固定 open，客户端说了不算
		Position:    draft.Position.Rounded(),       // 坐标收敛到 3 位小数，全库统一口径
		CreatedAt:   model.FormatTime(now),          // UTC，RFC3339
		UpdatedAt:   model.FormatTime(now),          // 初始与创建时间相同
	}

	// ⑤ 落库。写完这一刻起数据才真正存在（进程挂了也还在）。
	// 5 秒超时既管"库被锁"，也管"客户端已经断开，不必再做"。
	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()
	if err := s.store.Insert(ctx, rec); err != nil {
		s.writeError(w, r, model.Internal("写入记录失败: %v", err))
		return
	}

	// ⑦ 返回 201 + 整条记录（而不是只返回 id）。
	// 让手机端能核对"后端理解的和我想的一样"：priority 有没有被吞、坐标精度掉没掉、
	// status 是不是真的是 open。多返回两百字节，换一个可验证的闭环。
	writeJSON(w, http.StatusCreated, rec)
}

// toDraft 把请求体翻译成 model.Draft（"还没落库的一条记录"）。
//
// 职责边界很清楚，只管**类型层面**的事：
//   - 字段没传 → 留空，交给 Validate 统一报「缺少位置坐标」这类错误
//   - 类型不对（{"x":"NaN"}、数值溢出）→ 当场报「坐标不是有效数字」
//
// 业务规则一条都不在这里写，否则同一句话术会出现两处，迟早不一致。
func (r createRequest) toDraft() (model.Draft, *model.Error) {
	// 三个可选字符串先摊平：没传就是空串，合法性统一交给 Validate 判断
	d := model.Draft{
		Title:       derefString(r.Title),
		Description: derefString(r.Description),
		Priority:    derefString(r.Priority),
	}

	// 坐标要单独处理：得先分清"整个 position 没给"和"给了但某一轴没给"。
	// 这两种情况都不在这里报错，都留给 Validate 报「缺少位置坐标」——
	// 话术只在一个地方维护，才不会出现两种说法。
	if len(r.Position) == 0 || string(r.Position) == "null" {
		return d, nil // HasPosition 保持 false
	}

	// 三个轴分开接，同样是为了区分"没这个轴"和"这个轴的值是 0"。
	// 0 是合法坐标，不能拿 0 当"没传"的标记。
	var p struct {
		X *float64 `json:"x"`
		Y *float64 `json:"y"`
		Z *float64 `json:"z"`
	}
	if err := json.Unmarshal(r.Position, &p); err != nil {
		// 解不出来说明类型或范围不对（字符串 "NaN"、1e400 溢出等），
		// 这和"缺字段"是两回事，所以这里直接报「坐标不是有效数字」
		return d, model.InvalidArgument("坐标不是有效数字")
	}
	// 少任意一个轴，仍按"缺少位置坐标"处理
	if p.X == nil || p.Y == nil || p.Z == nil {
		return d, nil
	}

	d.Position = model.Position{X: *p.X, Y: *p.Y, Z: *p.Z}
	d.HasPosition = true
	return d, nil
}

// ---------------------------------------------------------------
// PATCH /api/inspections/{id}
// ---------------------------------------------------------------

// handlePatch 修改一条记录的处理状态，是闭环的最后一跳。
//
// 三关口，顺序固定，理由是契约冻结点 3「只返回第一个错误」：
//
//	① body 合法（400）→ ② 记录存在（404）→ ③ 状态允许（409）
//
// 顺序不能反：请求本身都不合法时，去纠结记录在不在没有意义。
// 这一层也不"顺手"多做点事——能不能改由 model.CanTransition 说了算。
func (s *Server) handlePatch(w http.ResponseWriter, r *http.Request) {
	// 路径上的 id，来自手机端或管理端列表里那一行
	id := r.PathValue("id")

	// ①-A 先解成"键 → 原始值"，而不是结构体。
	// 因为要检查客户端到底传了哪些键，才能实现"传了 status 以外的字段就 400"。
	var raw map[string]json.RawMessage
	if err := decodeJSON(w, r, &raw); err != nil {
		s.writeError(w, r, model.InvalidArgument("请求体不是合法 JSON"))
		return
	}
	// ①-B 空对象 {} 或空请求体：没有 status 可改，明确报错比静默成功好
	if len(raw) == 0 {
		s.writeError(w, r, model.InvalidArgument("请求体缺少 status"))
		return
	}
	// ①-C 契约冻结点 5：只允许 status 一个字段。
	// 不静默忽略的理由很实际——静默忽略会把"我改了怎么没生效"变成最难查的问题；
	// 当场报错则让错误在发请求的那一刻就暴露。
	for k := range raw {
		if k != "status" {
			s.writeError(w, r, model.InvalidArgument("只允许修改 status 字段，收到: %s", k))
			return
		}
	}
	// ①-D 值必须是字符串，且是三个枚举之一
	var statusStr string
	if err := json.Unmarshal(raw["status"], &statusStr); err != nil {
		s.writeError(w, r, model.InvalidArgument("状态必须是 open、in_progress 或 resolved"))
		return
	}
	to, ok := model.ParseStatus(statusStr)
	if !ok {
		s.writeError(w, r, model.InvalidArgument("状态必须是 open、in_progress 或 resolved"))
		return
	}

	// 走到这里 body 已经合法，后面才允许碰数据库
	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()

	// ② 记录必须存在。
	// 两个失败要分开：查库出错（500，我们的问题）vs 查不到（404，客户端拿了过期 id）
	current, found, err := s.store.Get(ctx, id)
	if err != nil {
		s.writeError(w, r, model.Internal("读取记录失败: %v", err))
		return
	}
	if !found {
		s.writeError(w, r, model.NotFound("记录不存在"))
		return
	}

	// ②-B 幂等：目标状态和当前状态相同，直接 200，**不执行任何写操作**。
	// 两个理由：管理员手快点两次不该看到红字；真的没变就不该留下
	// "最近处理时间被刷新"这种假痕迹——那会让排障时误以为有人动过。
	if current.Status == to {
		writeJSON(w, http.StatusOK, current)
		return
	}

	// ③ 状态机：能不能这么改由 model 决定，handler 不做业务判断。
	// 前端因此也不判断——它只负责把 409 的 message 显示出来，规则改了前端不用动。
	if !model.CanTransition(current.Status, to) {
		s.writeError(w, r, model.TransitionError(current.Status, to))
		return
	}

	// ④ 真正写库：只改 status 和 updated_at 两列，然后取回更新后的整条。
	// 返回整条而不是只返回"成功了"，是因为契约要求 200 + 更新后整条，
	// 管理端也靠这条响应覆盖本地那一行，不自己拼。
	updated, found, err := s.store.UpdateStatus(ctx, id, to, model.FormatTime(model.Now()))
	if err != nil {
		s.writeError(w, r, model.Internal("更新状态失败: %v", err))
		return
	}
	if !found {
		// 极小概率：刚才 Get 还在，这一瞬间被别的东西删了。
		// 不省这个分支——省掉的话这里会静默返回一条空记录。
		s.writeError(w, r, model.NotFound("记录不存在"))
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// ---------------------------------------------------------------
// 兜底 404
// ---------------------------------------------------------------

// handleNotFound 接住所有没匹配上路由的请求，返回契约形状的错误体。
//
// 唯一价值：路径写错时也返回 JSON，而不是 Go 默认的纯文本 404。
// 顺手把方法路径回显，方便调用方确认自己到底请求了什么。
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, r, model.NotFound("没有这个端点: %s %s", r.Method, r.URL.Path))
}

// ---------------------------------------------------------------
// 唯一的错误出口
// ---------------------------------------------------------------

// errorBody 是错误体内层：code 给机器分流，message 给人看。
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// errorEnvelope 是外层，把上面那个包进 "error"，最终形状 {"error":{"code":"…","message":"…"}}。
//
// 分层的理由：成功响应是"记录本身"，失败响应有明确的顶层分界，
// 前端不必靠"有没有 code 字段"去猜这次成功还是失败。
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// writeError 是全项目唯一写错误响应的地方（架构设计 S5：统一错误出口）。
//
// 它做两件事：
//  1. 把业务错误码翻译成 HTTP 状态码（400 / 404 / 409 / 500）
//  2. 内部错误只把原始原因写进日志，响应体换成一句人话
//
// 为什么必须收成一个出口：错误体形状一变三端都要改。
// 每个 handler 各自拼 JSON，迟早出现两种形状，客户端解析必然踩坑。
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	// 取出我们自己的错误类型；取不到说明来自别处（比如 SQL 驱动），
	// 一律按内部错误处理，不把底层细节暴露给调用方。
	var me *model.Error
	if !errors.As(err, &me) {
		me = model.Internal("服务异常，请重试")
	}

	// 默认 500，只有认识的错误码才降级。
	// 这样将来新增错误码时，不会因为漏写分支而意外变成"成功"。
	status := http.StatusInternalServerError
	switch me.Code {
	case model.CodeInvalidArgument:
		status = http.StatusBadRequest // 400：字段不对，改一下就能过
	case model.CodeNotFound:
		status = http.StatusNotFound // 404：记录不存在，刷新一下
	case model.CodeInvalidTransition:
		status = http.StatusConflict // 409：状态不允许这么变
	case model.CodeInternal:
		// 500：我们的错。日志留原始原因（含 SQL 细节），响应体只给一句人话
		s.log.Error("内部错误", "path", r.URL.Path, "err", err)
		me = model.Internal("服务异常，请重试")
	}

	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: string(me.Code), Message: me.Message}})
}

// writeJSON 是所有响应的统一出口：设 Content-Type、写状态码、把对象编码成 JSON。
//
// 顺序不能换：响应头必须在状态码之前设，状态码一旦写出去就改不了了。
// 所以编码失败时这里只能放弃——真实项目该记一条日志；
// 这个项目走不到这条路（写的是内存 buffer，不会失败），留空并写明原因，不假装处理。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8") // 必须带 charset，否则中文可能被当 latin-1
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}

// decodeJSON 把请求体解进 dst 指向的对象。
//
// 三个行为：
//  1. 给 body 套 64KB 上限，超了在读取阶段就失败（不是先读进内存再判断）
//  2. 流式解码，不需要先把整个 body 读成一块内存
//  3. 空请求体转成一句人话的错，上层统一按"请求体不是合法 JSON"处理即可，不必各自判空
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("请求体为空") // 完全没带请求体，例如 curl 忘了 -d
		}
		return err
	}
	return nil
}

// derefString 把"可能没传的字符串"取成普通字符串：没传就是空串。
//
// 有它之后，"字段缺失"在上层只表现为空串，
// 合法性判断集中在 model.Validate 一处，不用每个字段各写一遍 if。
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
