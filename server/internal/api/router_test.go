// HTTP 层的测试。这一层以前一条测试都没有——model 有 13 项单测，
// 但"路由有没有挂对、状态码对不对、错误体形状一致不一致"全靠手工 curl 和 smoke.ps1。
//
// 手工 curl 的问题不是不准，是**不会自动重跑**：改了路由忘了验证，
// 要等到管理端页面白屏才发现。所以这里把契约里每一条能断言的都钉成测试。
//
// 测试只碰 HTTP 接口、不碰内部结构：它验证的是"契约"，不是"实现方式"。
// 换句话说，将来把 ServeMux 换成别的路由器，这些测试应当照样通过。
package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ar-inspection/server/internal/model"
	"ar-inspection/server/internal/store"
)

// newTestServer 装一个带临时库的 Server。
//
// 每个测试一个独立库文件（t.TempDir 会在测试结束时自动清掉），
// 这样测试之间不会互相污染，也绝不会碰到开发用的 data/inspections.db。
func newTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { st.Close() }) // 让 Windows 能删掉临时文件

	// 测试里不需要日志噪音，但也不能传 nil——middleware 会用它
	return New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), "test-version")
}

// do 发一个请求并返回完整的响应记录器。
func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// errEnvelope 对应契约里的错误体 {"error":{"code":…,"message":…}}。
type errEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeErr(t *testing.T, w *httptest.ResponseRecorder) errEnvelope {
	t.Helper()
	var e errEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("错误体不是合法 JSON: %v (body=%q)", err, w.Body.String())
	}
	return e
}

// assertError 断言"状态码 + 错误码"这一对。契约里这两者必须同时对上：
// 只对状态码不够（形状可能变了），只对 code 也不够（客户端可能按状态码分流）。
func assertError(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantCode model.Code) {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("状态码 = %d，期望 %d (body=%s)", w.Code, wantStatus, w.Body.String())
	}
	e := decodeErr(t, w)
	if e.Error.Code != string(wantCode) {
		t.Fatalf("错误码 = %q，期望 %q (message=%q)", e.Error.Code, wantCode, e.Error.Message)
	}
	if e.Error.Message == "" {
		t.Fatal("错误体里没有 message —— 契约要求给人看的话不能空")
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q，期望 application/json", ct)
	}
}

// ---------------------------------------------------------------
// GET /api/health
// ---------------------------------------------------------------

func TestHealth(t *testing.T) {
	w := do(t, newTestServer(t).Handler(), http.MethodGet, "/api/health", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", w.Code)
	}
	var got healthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是健康检查结构: %v", err)
	}
	if got.Status != "ok" || got.DB != "ok" || got.Version != "test-version" {
		t.Fatalf("健康检查内容不对: %+v", got)
	}
}

// ---------------------------------------------------------------
// GET /api/inspections
// ---------------------------------------------------------------

// 空库必须是 {"items":[]}，不能是 {"items":null}。
// 对前端来说"一条都没有"和"字段不见了"是两种完全不同的情况。
func TestListEmptyIsArrayNotNull(t *testing.T) {
	w := do(t, newTestServer(t).Handler(), http.MethodGet, "/api/inspections", "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", w.Code)
	}
	if body := strings.TrimSpace(w.Body.String()); body != `{"items":[]}` {
		t.Fatalf("空列表响应 = %s，期望 {\"items\":[]}", body)
	}
}

// ---------------------------------------------------------------
// POST /api/inspections
// ---------------------------------------------------------------

func TestCreateThenList(t *testing.T) {
	h := newTestServer(t).Handler()

	w := do(t, h, http.MethodPost, "/api/inspections",
		`{"title":"  入口墙面破损  ","description":"左侧墙体裂缝","priority":"high","position":{"x":0.42123,"y":-1.2,"z":1.2609}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d，期望 201 (body=%s)", w.Code, w.Body.String())
	}

	var created model.Inspection
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("响应不是一条记录: %v", err)
	}
	// 服务端生成的字段，一个都不能少
	if !strings.HasPrefix(created.ID, "insp_") || len(created.ID) != len("insp_")+26 {
		t.Fatalf("id = %q，期望 insp_ + 26 位 ULID", created.ID)
	}
	if created.Status != model.StatusOpen {
		t.Fatalf("新记录状态 = %q，期望 open", created.Status)
	}
	if created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatal("时间戳没生成")
	}
	if created.CreatedAt != created.UpdatedAt {
		t.Fatalf("createdAt(%s) 应等于 updatedAt(%s)", created.CreatedAt, created.UpdatedAt)
	}
	// 标题去掉首尾空白，坐标收敛到 3 位小数
	if created.Title != "入口墙面破损" {
		t.Fatalf("标题 = %q，期望已 trim", created.Title)
	}
	wantPos := model.Position{X: 0.421, Y: -1.2, Z: 1.261}
	if created.Position != wantPos {
		t.Fatalf("坐标 = %+v，期望 %+v（3 位小数）", created.Position, wantPos)
	}

	// 写完立刻能读回来：这是"三端同一份数据"的最小证据
	w = do(t, h, http.MethodGet, "/api/inspections", "")
	var list listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("列表解析失败: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("列表里没有刚建的记录: %+v", list.Items)
	}
}

// 客户端传了只有服务端才能决定的字段（id / status / 时间戳）时，静默丢弃、不报错。
// 这是契约明确允许的，写进测试免得以后有人"顺手"改成 400。
func TestCreateIgnoresServerOwnedFields(t *testing.T) {
	w := do(t, newTestServer(t).Handler(), http.MethodPost, "/api/inspections",
		`{"id":"insp_HACK","status":"resolved","createdAt":"2000-01-01T00:00:00Z","title":"带脏字段","priority":"low","position":{"x":0,"y":0,"z":0}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d，期望 201 (body=%s)", w.Code, w.Body.String())
	}
	var created model.Inspection
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if created.ID == "insp_HACK" || created.Status != model.StatusOpen || strings.HasPrefix(created.CreatedAt, "2000") {
		t.Fatalf("服务端字段被客户端左右了: %+v", created)
	}
}

// 校验顺序是契约的一部分：同一个坏请求永远得到同一句话。
func TestCreateValidation(t *testing.T) {
	h := newTestServer(t).Handler()

	cases := []struct {
		name    string
		body    string
		message string // 期望的错误文案（含关键字即可）
	}{
		{"空标题", `{"title":"   ","priority":"high","position":{"x":0,"y":0,"z":0}}`, "标题不能为空"},
		{"标题超长", `{"title":"` + strings.Repeat("字", 101) + `","priority":"high","position":{"x":0,"y":0,"z":0}}`, "标题最多"},
		{"描述超长", `{"title":"t","description":"` + strings.Repeat("字", 501) + `","priority":"high","position":{"x":0,"y":0,"z":0}}`, "描述最多"},
		{"优先级不认识", `{"title":"t","priority":"urgent","position":{"x":0,"y":0,"z":0}}`, "优先级必须是"},
		{"缺坐标", `{"title":"t","priority":"high"}`, "缺少位置坐标"},
		{"坐标少一轴", `{"title":"t","priority":"high","position":{"x":0,"y":0}}`, "缺少位置坐标"},
		{"坐标不是数字", `{"title":"t","priority":"high","position":{"x":"NaN","y":0,"z":0}}`, "坐标不是有效数字"},
		{"请求体不是 JSON", `not json at all`, "请求体不是合法 JSON"},
		{"空请求体", ``, "请求体不是合法 JSON"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := do(t, h, http.MethodPost, "/api/inspections", c.body)
			assertError(t, w, http.StatusBadRequest, model.CodeInvalidArgument)
			if msg := decodeErr(t, w).Error.Message; !strings.Contains(msg, c.message) {
				t.Fatalf("错误文案 = %q，期望包含 %q", msg, c.message)
			}
		})
	}
}

// 一个校验失败之后，库里不能留下半条记录——写入口只有最后一步才落库。
func TestCreateValidationDoesNotWrite(t *testing.T) {
	h := newTestServer(t).Handler()
	do(t, h, http.MethodPost, "/api/inspections", `{"title":"","priority":"high","position":{"x":0,"y":0,"z":0}}`)
	w := do(t, h, http.MethodGet, "/api/inspections", "")
	if body := strings.TrimSpace(w.Body.String()); body != `{"items":[]}` {
		t.Fatalf("校验失败却写进了库: %s", body)
	}
}

// ---------------------------------------------------------------
// PATCH /api/inspections/{id}
// ---------------------------------------------------------------

// createOne 建一条记录并返回 id，后续状态机测试都从 open 开始。
func createOne(t *testing.T, h http.Handler) string {
	t.Helper()
	w := do(t, h, http.MethodPost, "/api/inspections",
		`{"title":"状态机测试","priority":"medium","position":{"x":1,"y":-1,"z":2}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("准备数据失败: %d %s", w.Code, w.Body.String())
	}
	var created model.Inspection
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("准备数据解析失败: %v", err)
	}
	return created.ID
}

func patchStatus(t *testing.T, h http.Handler, id, status string) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, h, http.MethodPatch, "/api/inspections/"+id, `{"status":"`+status+`"}`)
}

func TestPatchAllowsForwardAndRollback(t *testing.T) {
	h := newTestServer(t).Handler()
	id := createOne(t, h)

	// open → in_progress
	w := patchStatus(t, h, id, "in_progress")
	if w.Code != http.StatusOK {
		t.Fatalf("open→in_progress 状态码 = %d，期望 200 (%s)", w.Code, w.Body.String())
	}
	var got model.Inspection
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if got.Status != model.StatusInProgress {
		t.Fatalf("状态 = %q，期望 in_progress", got.Status)
	}

	// in_progress → resolved
	if w := patchStatus(t, h, id, "resolved"); w.Code != http.StatusOK {
		t.Fatalf("in_progress→resolved 状态码 = %d，期望 200", w.Code)
	}
	// resolved → open（唯一允许的回退）
	if w := patchStatus(t, h, id, "open"); w.Code != http.StatusOK {
		t.Fatalf("resolved→open 状态码 = %d，期望 200", w.Code)
	}
}

func TestPatchRejectsBackwardTransition(t *testing.T) {
	h := newTestServer(t).Handler()
	id := createOne(t, h)
	if w := patchStatus(t, h, id, "in_progress"); w.Code != http.StatusOK {
		t.Fatalf("准备 in_progress 失败: %d", w.Code)
	}
	// in_progress → open 会抹掉"曾有进展"这个事实，409
	w := patchStatus(t, h, id, "open")
	assertError(t, w, http.StatusConflict, model.CodeInvalidTransition)
}

// 同状态重复 PATCH 必须幂等：200，而且**不刷新 updatedAt**。
// 刷新了就会留下"刚刚有人动过"的假痕迹，排障时会被带偏。
func TestPatchIdempotentDoesNotTouchUpdatedAt(t *testing.T) {
	h := newTestServer(t).Handler()
	id := createOne(t, h)

	first := patchStatus(t, h, id, "in_progress")
	if first.Code != http.StatusOK {
		t.Fatalf("第一次 PATCH 失败: %d", first.Code)
	}
	var a model.Inspection
	if err := json.Unmarshal(first.Body.Bytes(), &a); err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	second := patchStatus(t, h, id, "in_progress")
	if second.Code != http.StatusOK {
		t.Fatalf("重复 PATCH 状态码 = %d，期望 200（幂等）", second.Code)
	}
	var b model.Inspection
	if err := json.Unmarshal(second.Body.Bytes(), &b); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if a.UpdatedAt != b.UpdatedAt {
		t.Fatalf("幂等 PATCH 刷新了 updatedAt：%s → %s", a.UpdatedAt, b.UpdatedAt)
	}
}

func TestPatchRejectsUnknownField(t *testing.T) {
	h := newTestServer(t).Handler()
	id := createOne(t, h)

	// 契约冻结点：只允许 status 一个字段。多传别的必须当场报错，
	// 静默忽略会把"我改了怎么没生效"变成最难查的问题。
	w := do(t, h, http.MethodPatch, "/api/inspections/"+id, `{"status":"resolved","title":"偷改标题"}`)
	assertError(t, w, http.StatusBadRequest, model.CodeInvalidArgument)
}

func TestPatchRejectsBadBody(t *testing.T) {
	h := newTestServer(t).Handler()
	id := createOne(t, h)

	assertError(t, do(t, h, http.MethodPatch, "/api/inspections/"+id, `{}`),
		http.StatusBadRequest, model.CodeInvalidArgument)
	assertError(t, do(t, h, http.MethodPatch, "/api/inspections/"+id, `{"status":"done"}`),
		http.StatusBadRequest, model.CodeInvalidArgument)
}

func TestPatchUnknownID(t *testing.T) {
	w := patchStatus(t, newTestServer(t).Handler(), "insp_NOTEXIST", "resolved")
	assertError(t, w, http.StatusNotFound, model.CodeNotFound)
}

// ---------------------------------------------------------------
// 兜底：未知路径 404 / 方法不对 405
// ---------------------------------------------------------------

func TestUnknownPathIsJSON(t *testing.T) {
	w := do(t, newTestServer(t).Handler(), http.MethodGet, "/api/nope", "")
	assertError(t, w, http.StatusNotFound, model.CodeNotFound)
	if msg := decodeErr(t, w).Error.Message; !strings.Contains(msg, "/api/nope") {
		t.Fatalf("404 文案没回显路径: %q", msg)
	}
}

// 这条测试锁的是一个实测出来的坑：注册了 "/" 兜底之后，ServeMux 再也不返回 405，
// DELETE /api/inspections 会被吞成 404「没有这个端点」——指错了方向。
func TestMethodNotAllowedCarriesAllowHeader(t *testing.T) {
	h := newTestServer(t).Handler()

	w := do(t, h, http.MethodDelete, "/api/inspections", "")
	assertError(t, w, http.StatusMethodNotAllowed, model.CodeMethodNotAllowed)
	if allow := w.Header().Get("Allow"); allow != "GET, POST" {
		t.Fatalf("Allow = %q，期望 \"GET, POST\"", allow)
	}

	w = do(t, h, http.MethodGet, "/api/inspections/insp_X", "")
	assertError(t, w, http.StatusMethodNotAllowed, model.CodeMethodNotAllowed)
	if allow := w.Header().Get("Allow"); allow != "PATCH" {
		t.Fatalf("Allow = %q，期望 \"PATCH\"", allow)
	}
}

// ---------------------------------------------------------------
// 中间件：请求 id / CORS / panic 兜底
// ---------------------------------------------------------------

func TestRequestIDGenerated(t *testing.T) {
	w := do(t, newTestServer(t).Handler(), http.MethodGet, "/api/health", "")
	id := w.Header().Get("X-Request-Id")
	if len(id) != 24 {
		t.Fatalf("X-Request-Id = %q，期望 24 位十六进制", id)
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("X-Request-Id 含非十六进制字符: %q", id)
		}
	}
}

// 上游带了合法 id 就沿用：跨服务对账靠的就是同一条 id 串下去。
func TestRequestIDEchoed(t *testing.T) {
	h := newTestServer(t).Handler()
	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.Header.Set("X-Request-Id", "trace-abc_123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Header().Get("X-Request-Id"); got != "trace-abc_123" {
		t.Fatalf("X-Request-Id = %q，期望沿用上游值", got)
	}
}

// 脏 id 会被重写：这个值会进日志，不校验等于允许调用方往日志里写换行、伪造日志行。
func TestRequestIDRejectsDirtyValue(t *testing.T) {
	h := newTestServer(t).Handler()
	for _, dirty := range []string{"has space", "line\nbreak", strings.Repeat("a", 65)} {
		r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		r.Header.Set("X-Request-Id", dirty)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if got := w.Header().Get("X-Request-Id"); got == dirty {
			t.Fatalf("脏 id %q 被原样回显了", dirty)
		}
	}
}

func TestCORS(t *testing.T) {
	h := newTestServer(t).Handler()

	// 预检：浏览器发真请求之前先问一次
	r := httptest.NewRequest(http.MethodOptions, "/api/inspections", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("预检状态码 = %d，期望 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin = %q", got)
	}
	// 没 exposed 的话，浏览器里的 JS 读不到这个响应头（跨域默认只暴露少数几个）
	if !strings.Contains(w.Header().Get("Access-Control-Expose-Headers"), "X-Request-Id") {
		t.Fatalf("没暴露 X-Request-Id: %q", w.Header().Get("Access-Control-Expose-Headers"))
	}

	// 真请求也要带上 CORS 头，否则浏览器会把响应拦掉
	w = do(t, h, http.MethodGet, "/api/health", "")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("真请求缺 Allow-Origin: %q", got)
	}
}

// panic 兜底单独测：它要接住的是 handler 里的 nil 解引用这类致命错误。
// 没有这一层，客户端拿到的是连接被掐断，不是一个能解析的错误体。
func TestRecoveryTurnsPanicInto500(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		var p *int
		_ = *p // 故意 panic
	})
	h := withRecovery(logger, boom)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	assertError(t, w, http.StatusInternalServerError, model.CodeInternal)
}
