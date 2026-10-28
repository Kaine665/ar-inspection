// Package api 拥有全部 HTTP 知识：路径、方法、状态码、JSON 编解码，
// 以及**全项目唯一的错误出口**。
//
// 它不认识 SQL，也不知道表长什么样——那些是 store 的事。
// 依赖方向是单向的：api → store → model，反过来会编译不过。
//
// 给不熟悉 Go 的读者：这个文件里出现的 http.ResponseWriter / *http.Request
// 就是"响应"和"请求"这两个东西；Go 没有框架，标准库的 net/http 就是框架。
package api

// import 里前半段是标准库，后半段是本项目自己的包（都是 ar-inspection/server/… 开头）。
// 注意：Go 会报"导入了但没用到"的错，所以下面每一个包在这里都真的被用到了。
import (
	"encoding/json" // JSON 编解码
	"errors"        // errors.As / errors.Is：给错误做类型断言和比较
	"io"            // io.EOF：表示"读到了流的末尾"（这里用来识别空请求体）
	"log/slog"      // 结构化日志（Go 1.21+ 标准库）
	"net/http"      // HTTP 服务器与客户端
	"time"          // 时间与超时

	"ar-inspection/server/internal/model" // 业务知识：校验、状态机、ULID
	"ar-inspection/server/internal/store" // SQL 知识：增删查改
)

// maxBodyBytes 是单个请求体的上限，64KB。
// `64 << 10` 是位移写法：64 左移 10 位 = 64 × 1024 = 65536，等价于 64*1024。
// 常量（const）在编译期就固定，不能在运行时改。
// 为什么要有上限：任何一条巡检记录都远小于 1KB，给 64KB 已经极其宽松；
// 没有上限的话，一个恶意的巨大请求就能把内存吃光。
const maxBodyBytes = 64 << 10

// Server 是这个包的核心对象。
//
// 它是"一个 HTTP 服务的处理者"，只持有三样东西：
//   - store：真正读写数据库的对象（指针，见下面 New 的说明）
//   - log：结构化日志器，每个请求打一行
//   - version：版本号，/api/health 会把它原样返回，用来确认"部署的到底是哪个版本"
//
// 字段都是小写开头（store / log / version）——Go 里**首字母大小写决定可见性**：
// 小写 = 只有本包能访问（相当于 Java/TS 的 private），大写 = 包外也能访问。
type Server struct {
	store   *store.Store // 指向上面的"数据库句柄"，我们只借用，不拥有它的生命周期
	log     *slog.Logger // 日志器同样是借用
	version string       // 简单字符串，按值复制
}

// New 是 Server 的构造函数。
//
// Go 没有 class，也没有构造函数语法——"构造函数"只是一个普通函数，
// 约定叫 New，返回一个填好字段的对象指针。
//
// 为什么返回 *Server（指针）而不是 Server（值）：
// 指针是"同一个对象的引用"，多个 goroutine（并发请求）看到的是同一份 store 和 log；
// 如果返回值，每次传参都会复制一份，虽然这里复制也不出错，但语义上不对。
//
// 所有参数都必须传：Go 没有默认参数，也没有可选参数。
func New(st *store.Store, logger *slog.Logger, version string) *Server {
	// `&Server{...}` 是"复合字面量 + 取地址"，等价于 new 一个对象再逐字段赋值。
	// 这里用字段名: 值的写法，字段顺序变了也不会错。
	return &Server{store: st, log: logger, version: version}
}

// Handler 把所有路由装好，并套上中间件，返回一个 http.Handler。
//
// http.Handler 是 Go 标准库里最重要的接口（interface），只有一个方法：
//
//	ServeHTTP(w http.ResponseWriter, r *http.Request)
//
// 任何实现了这个方法的类型都能被 http.Server 拿去当处理器。
// 中间件（middleware）本质就是"包一层"：接收一个 Handler，返回一个新的 Handler。
//
// 路由用的是 Go 1.22 起标准库自带的能力：模式字符串可以写「方法 + 路径」，
// 路径里的 {id} 是占位符，handler 里用 r.PathValue("id") 取出来。
// 所以这个项目**不需要任何第三方路由库**（不用 gin / chi / echo）。
func (s *Server) Handler() http.Handler {
	// ServeMux 是标准库的"路由器"：把「方法 + 路径」映射到处理函数。
	mux := http.NewServeMux()

	// HandleFunc 接收两个东西：模式字符串、一个函数。
	// `s.handleHealth` 是"方法值"——把 s 这个接收者绑定上去，得到一个普通函数。
	// 注意这里没有括号，它不是调用，是把这个函数"交给"路由器。
	mux.HandleFunc("GET /api/health", s.handleHealth)            // 健康检查
	mux.HandleFunc("GET /api/inspections", s.handleList)         // 列表
	mux.HandleFunc("POST /api/inspections", s.handleCreate)      // 新建
	mux.HandleFunc("PATCH /api/inspections/{id}", s.handlePatch) // 改状态；{id} 是占位符

	// 兜底：路径没匹配上任何一条路由时走这里。
	// 不写这条的话，Go 会返回它自己的纯文本 404，形状不符合契约——
	// 客户端解析 JSON 会失败，那是最难查的一类问题。
	mux.HandleFunc("/", s.handleNotFound)

	// 中间件从内往外包：
	//   请求进来先过 withCORS（加跨域头，处理 OPTIONS 预检）
	//   再过 withLogging（记开始时间、事后记状态码和耗时）
	//   最后才进 mux 找 handler
	// 返回值的类型是 http.Handler（接口），不是 *Server——外面只关心"能不能处理请求"。
	return withCORS(withLogging(s.log, mux))
}

// ---------------------------------------------------------------
// 健康检查
// ---------------------------------------------------------------

// healthResponse 是 GET /api/health 的响应体。
//
// 反引号里的 `json:"status"` 叫**结构体标签（struct tag）**：
// 它告诉 encoding/json 这个字段在 JSON 里叫什么名字。
// 没有 tag 的话，Go 会直接用字段名（首字母大写 Status），那是契约不允许的。
// 这也是为什么"契约"在 Go 里落在 struct tag 上——改了 tag 就等于改了接口。
type healthResponse struct {
	Status  string `json:"status"`  // 固定 "ok"：进程活着就是 ok
	DB      string `json:"db"`      // "ok" 或 "error"：数据库到底通不通
	Version string `json:"version"` // 版本号，来自 main.go 的变量
}

// handleHealth 处理 GET /api/health。
//
// 参数解释（后面每个 handler 都一样，不再重复）：
//   - w http.ResponseWriter：写响应的地方，设置响应头、写状态码、写响应体都靠它
//   - r *http.Request：收到的请求，方法、路径、头、请求体都在里面（指针）
//
// 它**永远返回 200**：进程能跑这段代码，说明服务活着。
// 数据库坏没坏由 db 字段表达。
// 这样管理端能区分两种完全不同的故障：
//   - 请求失败 → "后端没起"（网络层就没通）
//   - 200 但 db=error → "后端在、库挂了"（这时读可能还行，写一定失败）
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// 给这次数据库检查套一个 2 秒的超时。
	// context 是 Go 里传递"取消 / 超时"的机制：一旦超时，底层 SQL 会立刻放弃。
	// `defer cancel()` 表示"函数返回时一定执行 cancel"——用来释放 context 占用的资源。
	// defer 的规则：后进先出，函数退出时执行，无论正常返回还是 panic。
	ctx, cancel := contextWithTimeout(r, 2*time.Second)
	defer cancel()

	db := "ok" // 先假定正常，出错再改——这种"默认 + 覆盖"的写法在这里比 if/else 清楚
	if err := s.store.Ping(ctx); err != nil {
		// if 里带一条短声明：err 只在 if 内部可见，出了 if 就没了，不会污染后面的作用域
		db = "error"
		// Warn 级别：不是致命错误（服务还活着），但值得看一眼
		s.log.Warn("health: 数据库 ping 失败", "err", err)
	}

	// 写 200 + JSON。healthResponse{...} 是复合字面量，字段可以只写一部分，其余取零值。
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", DB: db, Version: s.version})
}

// ---------------------------------------------------------------
// GET /api/inspections
// ---------------------------------------------------------------

// listResponse 是 GET /api/inspections 的响应体，把数组包在 items 里。
//
// 为什么不能直接返回 []model.Inspection：
// Unity 的 JsonUtility 反序列化不了"顶层数组"，只能处理对象。
// 包一层 {"items":[…]}, 三端都不需要写自定义解析器。
type listResponse struct {
	Items []model.Inspection `json:"items"`
}

// handleList 处理 GET /api/inspections，返回全部记录。
//
// 排序不在这里做：store.List 的 SQL 里已经写了 ORDER BY created_at DESC。
// 前端也不再排——"谁排"只能有一个答案，否则两处不一致时没法判断谁对。
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	// 查库比 ping 慢，给 5 秒；超时会让 SQL 放弃，避免请求堆积
	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()

	// Go 的多返回值：一次拿到结果和错误两个值，调用方必须显式处理错误。
	// err != nil 是 Go 里最常见的判断，几乎每一行都跟着它。
	items, err := s.store.List(ctx)
	if err != nil {
		// 用 %v 把原始错误拼进内部错误的 message 里——这份 message 只会进日志，
		// 响应体里会被 writeError 换成"服务异常，请重试"
		s.writeError(w, r, model.Internal("读取列表失败: %v", err))
		return
	}
	// store.List 保证返回空切片而不是 nil，所以 JSON 里是 [] 而不是 null。
	// 对前端来说"后端说一条都没有"和"字段不存在"是两件事，不能混淆。
	writeJSON(w, http.StatusOK, listResponse{Items: items})
}

// ---------------------------------------------------------------
// POST /api/inspections
// ---------------------------------------------------------------

// createRequest 是「新建一条记录」的请求体。
//
// 为什么字段用 *string（指针）而不是 string：
// 指针可以是 nil，用来表示**"客户端压根没传这个字段"**；
// 而 string 的零值是 ""，分不清"没传"和"传了空字符串"。
// 契约里这两种情况要报不同的错（"标题不能为空" vs 缺字段），所以必须能区分。
//
// 为什么 Position 用 json.RawMessage：
// 它是"原始 JSON 字节"，先不解析，留给 toDraft 去判断到底缺哪一轴、还是类型不对。
// 如果直接声明成 float64，{"x":"NaN"} 会在这一步就报错，我们没法给出契约要求的话术。
type createRequest struct {
	Title       *string         `json:"title"`
	Description *string         `json:"description"`
	Priority    *string         `json:"priority"`
	Position    json.RawMessage `json:"position"`
}

// handleCreate 处理 POST /api/inspections（整个链路唯一的写入口，只有手机端调）。
//
// 流程七步，顺序和契约第 5 节一致：
//
//	① 解 JSON → ② 翻译成 Draft → ③ 业务校验 → ④ 生成服务端字段
//	⑤ 落库 → ⑥ 出错就写错误体 → ⑦ 返回整条记录
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	// ① 把请求体解进 req。
	// var req createRequest 声明一个零值对象；&req 取地址传进去，让解码器往里写。
	// 解不出来（不是合法 JSON / 空体 / 超过 64KB）就到此为止，返回 400。
	var req createRequest
	if err := decodeJSON(w, r, &req); err != nil {
		s.writeError(w, r, model.InvalidArgument("请求体不是合法 JSON"))
		return
	}

	// ② 类型层面翻译。注意 toDraft 的接收者是 createRequest（值），不是 *Server——
	// 它跟这个 HTTP 服务无关，只是这段数据自己的事。
	draft, perr := req.toDraft()
	if perr != nil {
		// perr 是 *model.Error；这里的判断比 err != nil 更具体，因为 toDraft 只报一种错
		s.writeError(w, r, perr)
		return
	}
	// ③ 业务校验：标题、描述长度、优先级、坐标。命中第一条错就返回。
	if err := draft.Validate(); err != nil {
		s.writeError(w, r, err)
		return
	}

	// ④ 生成只有服务端能给的字段。
	// now 取一次就够了：createdAt 和 updatedAt 必须完全相等，
	// 分两次取时间会出现"创建时间比更新时间早几毫秒"这种假数据。
	now := model.Now()
	rec := model.Inspection{
		ID:          model.NewID(now),               // insp_ + ULID，含毫秒时间戳
		Title:       draft.NormalizeTitle(),         // trim 后的标题才落库
		Description: draft.Description,              // 原样保留，可空
		Priority:    model.Priority(draft.Priority), // 字符串转成枚举类型（底层还是 string）
		Status:      model.StatusOpen,               // 新记录固定 open，客户端说了不算
		Position:    draft.Position.Rounded(),       // 坐标收敛到 3 位小数
		CreatedAt:   model.FormatTime(now),          // UTC + RFC3339
		UpdatedAt:   model.FormatTime(now),          // 初始与创建时间相同
	}

	// ⑤ 落库。context 同样带 5 秒超时——客户端断开时 SQL 也就不必做完了。
	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()
	if err := s.store.Insert(ctx, rec); err != nil {
		s.writeError(w, r, model.Internal("写入记录失败: %v", err))
		return
	}

	// ⑦ 返回 201 + 整条记录（不是只返回 id）。
	// 让手机端能核对"后端理解的和我想的一样"：priority 有没有被吞、坐标精度掉没掉、
	// status 是不是真的是 open。多返回 200 字节，换来一个可验证的闭环。
	writeJSON(w, http.StatusCreated, rec)
}

// toDraft 把 HTTP 请求体翻译成 model.Draft（"还没落库的一条记录"）。
//
// 它只做**类型层面**的翻译：缺字段 → 留下来让 Validate 报错；类型不对 → 当场报错。
// 真正的业务规则（标题长度、枚举取值）全部在 model.Validate 里，这里一条都不写。
//
// 返回值是 (结果, 错误) 两个：Go 没有异常，"出错就多返回一个 error"是唯一惯例。
// 这里的错误类型是具体的 *model.Error（而不是 error），因为只有这一种可能。
func (r createRequest) toDraft() (model.Draft, *model.Error) {
	// 先把三个可选字符串填进去；derefString 负责"是 nil 就返回空串"。
	d := model.Draft{
		Title:       derefString(r.Title),
		Description: derefString(r.Description),
		Priority:    derefString(r.Priority),
	}

	// 坐标先看有没有：len == 0 表示字段不存在，"null" 表示显式传了 null。
	// 两种情况都不在这里报错，交给 Validate 统一报「缺少位置坐标」——
	// 报错话术只在一个地方维护，才不会出现两种说法。
	if len(r.Position) == 0 || string(r.Position) == "null" {
		return d, nil // HasPosition 保持 false
	}

	// 一个**匿名结构体**：只在这里用一次，没必要给它起名字。
	// 三个字段都是 *float64，同样是为了区分"没这个轴"和"轴的值为 0"。
	// 注意：0 是合法坐标，所以不能用 0 当"没传"的标记。
	var p struct {
		X *float64 `json:"x"`
		Y *float64 `json:"y"`
		Z *float64 `json:"z"`
	}
	// 把那段原始 JSON 字节解进匿名结构体。
	// 解不出来说明**类型或范围**不对，例如 {"x":"NaN"}（字符串）或 {"x":1e400}（溢出）——
	// 这跟"缺字段"是两回事，所以这里直接报"坐标不是有效数字"。
	if err := json.Unmarshal(r.Position, &p); err != nil {
		return d, model.InvalidArgument("坐标不是有效数字")
	}
	// 三个轴缺任意一个，都按"缺少位置坐标"处理（契约 6.3 的表格里写的是"缺 position 或 x 是 null"）
	if p.X == nil || p.Y == nil || p.Z == nil {
		return d, nil
	}

	// `*p.X` 是"解引用"：p.X 是 *float64（指向一个数），*p.X 才是那个数本身。
	d.Position = model.Position{X: *p.X, Y: *p.Y, Z: *p.Z}
	d.HasPosition = true
	return d, nil
}

// ---------------------------------------------------------------
// PATCH /api/inspections/{id}
//
// 校验顺序是固定的，理由是契约冻结点 3「只返回第一个错误」：
//  ① body 合法（400） → ② 记录存在（404） → ③ 状态机（409）
// 顺序不能反：请求本身都不合法时，纠结记录在不在没有意义。
// ---------------------------------------------------------------

// handlePatch 处理 PATCH /api/inspections/{id}：只允许改 status 一个字段。
//
// 路径里的 {id} 由路由匹配，用 r.PathValue("id") 取出（Go 1.22 起可用）。
// 注意 id 不需要自己解析、也不用正则——路由器已经切好了。
func (s *Server) handlePatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id") // 例如 insp_06GQZS9QZCMPWK52440ZRZY6ZK

	// 解成一个"键 → 原始 JSON"的 map，而不是结构体：
	// 因为我们要先知道**客户端到底传了哪些键**，才能实现"传了别的字段就 400"。
	var raw map[string]json.RawMessage
	if err := decodeJSON(w, r, &raw); err != nil {
		s.writeError(w, r, model.InvalidArgument("请求体不是合法 JSON"))
		return
	}
	// 空对象 {} 或空请求体：没有 status 可改，明确报错比静默成功好
	if len(raw) == 0 {
		s.writeError(w, r, model.InvalidArgument("请求体缺少 status"))
		return
	}
	// 契约冻结点 5：传了 status 以外的字段 → 400，不静默忽略。
	// 静默忽略会让"我改了怎么没生效"变成最难查的那类问题；
	// 明确报错则让错误在发请求的那一刻就暴露。
	// for k := range raw 是遍历 map 的键（只需要键时，值可以省略成 _）。
	for k := range raw {
		if k != "status" {
			s.writeError(w, r, model.InvalidArgument("只允许修改 status 字段，收到: %s", k))
			return
		}
	}

	// 取出 status 的值再解成字符串。
	// 这一步失败说明它不是字符串（比如 {"status":123} 或 {"status":null}）。
	var statusStr string
	if err := json.Unmarshal(raw["status"], &statusStr); err != nil {
		s.writeError(w, r, model.InvalidArgument("状态必须是 open、in_progress 或 resolved"))
		return
	}
	// 枚举校验。ParseStatus 返回 (值, 是否合法) —— Go 里没有 Optional，
	// "可能失败"的转换通常用这种 (T, bool) 形式，而不是返回 error。
	to, ok := model.ParseStatus(statusStr)
	if !ok {
		s.writeError(w, r, model.InvalidArgument("状态必须是 open、in_progress 或 resolved"))
		return
	}

	// 走到这里 body 已经合法，后面才允许碰数据库
	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()

	// 查这条记录在不在。三个返回值分别是：记录、是否存在、是否出错。
	// 为什么要区分"不存在"和"查库出错"：前者是 404（客户端的问题），后者是 500（我们的问题）。
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
	// 真的没变就不该留下"最近处理时间被刷新"这种假痕迹（契约冻结点 1）。
	// 注意这里是提前 return，不执行任何 UPDATE。
	if current.Status == to {
		writeJSON(w, http.StatusOK, current)
		return
	}

	// 状态机校验：能不能这么改由 model 决定，handler 不做业务判断。
	// 前端也因此不需要判断——它只负责把 409 的 message 显示出来。
	if !model.CanTransition(current.Status, to) {
		s.writeError(w, r, model.TransitionError(current.Status, to))
		return
	}

	// 真正写库：只改 status 和 updated_at 两列。
	// 返回更新后的整条，因为响应要带全量对象（契约要求 200 + 更新后整条）。
	updated, found, err := s.store.UpdateStatus(ctx, id, to, model.FormatTime(model.Now()))
	if err != nil {
		s.writeError(w, r, model.Internal("更新状态失败: %v", err))
		return
	}
	if !found {
		// 极小概率事件：刚才 Get 还在，这一瞬间被别人删了。
		// 不省这个分支——省掉的话这里会静默返回一条零值记录。
		s.writeError(w, r, model.NotFound("记录不存在"))
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// ---------------------------------------------------------------
// 兜底 404
// ---------------------------------------------------------------

// handleNotFound 接住所有没匹配上的路径（注册在 "/" 上）。
//
// 它存在的原因只有一个：让"路径写错"也返回契约形状的错误体。
// 否则 Go 默认的 404 是纯文本，客户端 JSON 解析会失败。
// r.Method / r.URL.Path 原样回给调用方，方便定位到底请求了什么。
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, r, model.NotFound("没有这个端点: %s %s", r.Method, r.URL.Path))
}

// ---------------------------------------------------------------
// 唯一的错误出口
// ---------------------------------------------------------------

// errorBody 是错误体的内层：给机器看的 code + 给人看的 message。
type errorBody struct {
	Code    string `json:"code"`    // invalid_argument / not_found / invalid_transition / internal
	Message string `json:"message"` // 中文，可直接显示给用户
}

// errorEnvelope 是错误体的外层，把上面那个包进 "error" 键里。
//
// 最终形状：{"error":{"code":"…","message":"…"}}
// 为什么要嵌套：成功响应是"记录本身"，失败响应有明确分界；
// 前端不必靠"有没有 code 字段"来猜成功还是失败（那种写法上线后才炸）。
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// writeError 是全项目**唯一**写错误响应的地方（架构设计里的 S5：统一错误出口）。
//
// 它做两件事：
//  1. 把业务错误码（model.Code）翻译成 HTTP 状态码
//  2. 内部错误只往日志里写原始原因，响应体里换成一句人话
//
// 为什么全项目只能有这一个出口：错误体形状一变，三端都要改。
// 如果每个 handler 自己拼 JSON，迟早会出现两种形状。
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	// errors.As 是"从错误里取出具体类型"：把 err 断言成 *model.Error 并写进 me。
	// 参数传的是 &me（指针的指针），因为它需要修改 me 本身。
	// 注意这里声明 `var me *model.Error` 时 me 是 nil——断言失败它就还是 nil。
	var me *model.Error
	if !errors.As(err, &me) {
		// 不是我们自己的错误类型（比如 SQL 驱动抛的），一律当内部错误处理
		me = model.Internal("服务异常，请重试")
	}

	// 默认 500，只有明确认识的错误码才降级。这种"默认最严"的写法，
	// 好处是将来新增错误码时不会意外变成 200。
	status := http.StatusInternalServerError
	switch me.Code {
	case model.CodeInvalidArgument:
		status = http.StatusBadRequest // 400
	case model.CodeNotFound:
		status = http.StatusNotFound // 404
	case model.CodeInvalidTransition:
		status = http.StatusConflict // 409
	case model.CodeInternal:
		// 内部错误：日志里留原始原因（含 SQL 细节），响应体只给一句人话。
		// 把 SQL/文件路径抛给客户端既不专业也不安全。
		s.log.Error("内部错误", "path", r.URL.Path, "err", err)
		me = model.Internal("服务异常，请重试")
	}

	// string(me.Code) 是"类型转换"：把 model.Code（本质是 string）转回 string，
	// 这样 JSON 里输出的是 "invalid_argument" 而不是枚举类型名。
	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: string(me.Code), Message: me.Message}})
}

// writeJSON 是所有成功响应和错误响应共同的"写出去"动作。
//
// 三行代码对应 HTTP 响应的三个部分，顺序不能换：
//  1. Header().Set：设置响应头（必须在写状态码之前）
//  2. WriteHeader：写状态码（一旦写了，头就发出去了，之后不能再改）
//  3. Encoder.Encode：把结构体编码成 JSON 写进 body
//
// 参数 v any 里的 any 就是 interface{}（Go 1.18 起的别名），意思是"任何类型"。
func writeJSON(w http.ResponseWriter, status int, v any) {
	// charset=utf-8 是必须的：中文标题如果被当成 latin-1 解码会变乱码
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// Encode 会自己加一个换行（curl 看到的那种末尾空行就是它）
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// 此时响应头已经发出去了，无法再改状态码，只能放弃。
		// 真实项目里这里应该记一条日志；这个项目不可能走到（写内存 buffer 不会失败），
		// 所以留空并写明原因，而不是假装处理了。
		return
	}
}

// decodeJSON 把请求体解进 dst 指定的对象。
//
// 参数 dst any：传的是目标对象的**地址**（&req），解码器才知道往哪写。
// 三个细节：
//  1. http.MaxBytesReader 给 body 套上限，超了就在读的时候报错（而不是先吃进内存再判断）
//  2. json.NewDecoder 比 json.Unmarshal 更适合读 HTTP body：它是流式的，不需要先读成 []byte
//  3. 空 body 会返回 io.EOF，这里转成一句人话，方便上层统一报"请求体不是合法 JSON"
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			// 完全没有请求体（比如 curl 没带 -d）
			return errors.New("请求体为空")
		}
		return err
	}
	return nil
}

// derefString 把 *string 安全地取成 string：nil 就当空串。
//
// 这是个小工具，但它是"可选字段"这套写法的收口处：
// 调用方永远拿到一个能直接用的 string，不必在每个字段上写 if 判断。
// `*p` 是解引用；写之前必须判 nil，否则解引用 nil 指针会 panic（Go 里唯一常见的崩溃来源之一）。
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
