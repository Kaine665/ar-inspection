// Package api 拥有全部 HTTP 知识：路径、方法、状态码、JSON 编解码、
// 以及**唯一的错误出口**。它不认识 SQL，也不知道表长什么样。
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

const maxBodyBytes = 64 << 10 // 64KB，远大于任何一条巡检记录

type Server struct {
	store   *store.Store
	log     *slog.Logger
	version string
}

func New(st *store.Store, logger *slog.Logger, version string) *Server {
	return &Server{store: st, log: logger, version: version}
}

// Handler 组装路由。用 Go 1.22 起 ServeMux 支持的「方法 + 路径」模式，
// 所以不需要任何第三方路由库。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/inspections", s.handleList)
	mux.HandleFunc("POST /api/inspections", s.handleCreate)
	mux.HandleFunc("PATCH /api/inspections/{id}", s.handlePatch)
	// 兜底：没匹配上的路径也返回契约形状的错误体，而不是 Go 默认的纯文本 404
	mux.HandleFunc("/", s.handleNotFound)

	return withCORS(withLogging(s.log, mux))
}

// ---------------------------------------------------------------
// 健康检查
// ---------------------------------------------------------------

type healthResponse struct {
	Status  string `json:"status"`
	DB      string `json:"db"`
	Version string `json:"version"`
}

// handleHealth 恒返回 200：进程还活着就是 200，数据库坏没坏看 db 字段。
// 这样管理端能区分「后端没起」和「后端在、库挂了」这两种完全不同的故障。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 2*time.Second)
	defer cancel()

	db := "ok"
	if err := s.store.Ping(ctx); err != nil {
		db = "error"
		s.log.Warn("health: 数据库 ping 失败", "err", err)
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", DB: db, Version: s.version})
}

// ---------------------------------------------------------------
// GET /api/inspections
// ---------------------------------------------------------------

type listResponse struct {
	Items []model.Inspection `json:"items"`
}

// 包一层 {"items":[…]} 不是为了好看：Unity 的 JsonUtility 反序列化不了顶层数组（契约 6.1）。
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()

	items, err := s.store.List(ctx)
	if err != nil {
		s.writeError(w, r, model.Internal("读取列表失败: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Items: items})
}

// ---------------------------------------------------------------
// POST /api/inspections
// ---------------------------------------------------------------

// createRequest 用指针 + RawMessage 而不是直接用 string/float64：
// 只有这样才分得清「字段没传」「传了 null」「传了错类型」三种情况，
// 它们对应契约里三条不同的错误信息。
type createRequest struct {
	Title       *string         `json:"title"`
	Description *string         `json:"description"`
	Priority    *string         `json:"priority"`
	Position    json.RawMessage `json:"position"`
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeJSON(w, r, &req); err != nil {
		s.writeError(w, r, model.InvalidArgument("请求体不是合法 JSON"))
		return
	}

	draft, perr := req.toDraft()
	if perr != nil {
		s.writeError(w, r, perr)
		return
	}
	if err := draft.Validate(); err != nil {
		s.writeError(w, r, err)
		return
	}

	now := model.Now()
	rec := model.Inspection{
		ID:          model.NewID(now),
		Title:       draft.NormalizeTitle(),
		Description: draft.Description,
		Priority:    model.Priority(draft.Priority),
		Status:      model.StatusOpen, // 新记录固定 open，客户端说了不算
		Position:    draft.Position.Rounded(),
		CreatedAt:   model.FormatTime(now),
		UpdatedAt:   model.FormatTime(now),
	}

	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()
	if err := s.store.Insert(ctx, rec); err != nil {
		s.writeError(w, r, model.Internal("写入记录失败: %v", err))
		return
	}

	// 返回整条而不是只返回 id：让手机端能核对「后端理解的和我想的一样」
	// （priority 有没有被吞、坐标精度掉没掉）。
	writeJSON(w, http.StatusCreated, rec)
}

// toDraft 把请求体翻译成 model.Draft。
// 这里只做「类型层面」的翻译，业务校验全部交给 model.Validate。
func (r createRequest) toDraft() (model.Draft, *model.Error) {
	d := model.Draft{
		Title:       derefString(r.Title),
		Description: derefString(r.Description),
		Priority:    derefString(r.Priority),
	}
	if len(r.Position) == 0 || string(r.Position) == "null" {
		return d, nil // 没给坐标 → Validate 报「缺少位置坐标」
	}

	var p struct {
		X *float64 `json:"x"`
		Y *float64 `json:"y"`
		Z *float64 `json:"z"`
	}
	if err := json.Unmarshal(r.Position, &p); err != nil {
		// 例如 {"x":"NaN"} 或 {"x":1e400}：类型/范围不对，不是「缺字段」
		return d, model.InvalidArgument("坐标不是有效数字")
	}
	if p.X == nil || p.Y == nil || p.Z == nil {
		return d, nil // 少一个轴 → 同样报「缺少位置坐标」
	}
	d.Position = model.Position{X: *p.X, Y: *p.Y, Z: *p.Z}
	d.HasPosition = true
	return d, nil
}

// ---------------------------------------------------------------
// PATCH /api/inspections/{id}
//
// 校验顺序是固定的，理由是契约冻结点 3「只返回第一个错误」：
//   ① body 合法（400） → ② 记录存在（404） → ③ 状态机（409）
// 顺序不能反：请求本身都不合法时，纠结记录在不在没有意义。
// ---------------------------------------------------------------

func (s *Server) handlePatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var raw map[string]json.RawMessage
	if err := decodeJSON(w, r, &raw); err != nil {
		s.writeError(w, r, model.InvalidArgument("请求体不是合法 JSON"))
		return
	}
	if len(raw) == 0 {
		s.writeError(w, r, model.InvalidArgument("请求体缺少 status"))
		return
	}
	// 契约冻结点 5：传了 status 以外的字段 → 400，不做静默忽略。
	// 静默忽略会让「改了却没生效」变成最难查的那类问题。
	for k := range raw {
		if k != "status" {
			s.writeError(w, r, model.InvalidArgument("只允许修改 status 字段，收到: %s", k))
			return
		}
	}

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

	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()

	current, found, err := s.store.Get(ctx, id)
	if err != nil {
		s.writeError(w, r, model.Internal("读取记录失败: %v", err))
		return
	}
	if !found {
		s.writeError(w, r, model.NotFound("记录不存在"))
		return
	}

	// 幂等：同一个状态再点一次，返回 200 且**不动 updatedAt**。
	// 真的没变就不该留下「最近处理时间被刷新」这种假痕迹（契约冻结点 1）。
	if current.Status == to {
		writeJSON(w, http.StatusOK, current)
		return
	}

	if !model.CanTransition(current.Status, to) {
		s.writeError(w, r, model.TransitionError(current.Status, to))
		return
	}

	updated, found, err := s.store.UpdateStatus(ctx, id, to, model.FormatTime(model.Now()))
	if err != nil {
		s.writeError(w, r, model.Internal("更新状态失败: %v", err))
		return
	}
	if !found {
		s.writeError(w, r, model.NotFound("记录不存在"))
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// ---------------------------------------------------------------
// 兜底 404
// ---------------------------------------------------------------

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, r, model.NotFound("没有这个端点: %s %s", r.Method, r.URL.Path))
}

// ---------------------------------------------------------------
// 唯一的错误出口
// ---------------------------------------------------------------

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// writeError 是全项目**唯一**写错误响应的地方（S5：统一错误出口）。
// 它做两件事：把 model.Error 翻译成 HTTP 状态码；把内部错误记进日志但不外泄。
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var me *model.Error
	if !errors.As(err, &me) {
		me = model.Internal("服务异常，请重试")
	}

	status := http.StatusInternalServerError
	switch me.Code {
	case model.CodeInvalidArgument:
		status = http.StatusBadRequest
	case model.CodeNotFound:
		status = http.StatusNotFound
	case model.CodeInvalidTransition:
		status = http.StatusConflict
	case model.CodeInternal:
		// 内部错误：日志里留原始原因，响应体只给一句人话，别把 SQL 细节抛给客户端
		s.log.Error("内部错误", "path", r.URL.Path, "err", err)
		me = model.Internal("服务异常，请重试")
	}

	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: string(me.Code), Message: me.Message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// 响应头已经发出去了，这里只能记一笔，无法再改状态码
		return
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("请求体为空")
		}
		return err
	}
	return nil
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
