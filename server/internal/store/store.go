// Package store 拥有全部 SQL 知识：建表、四个增删查改动作、连接怎么开。
//
// 它不认识 HTTP、不认识 JSON、不认识状态码，只认识表。想换存储（比如换 Postgres），
// 改这一个包就够，api 和 model 一行都不用动。
//
// 关于命名的一个说明：这里的方法叫 List / Get / Insert / UpdateStatus，
// 没有写成 GetInspections / get_inspection_by_id。因为**主语在包名上**——
// store.Get(...) 读起来就是"从存储取一条"。表名写在 SQL 里，函数名不带。
// 将来真有第二张表，做法是拆成两个包（store/inspection、store/event），
// 而不是在一个文件里堆表名前缀。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// 这一行前面的下划线表示"不引用它的名字，只要它被引入时的副作用"。
	// 这个包在被导入时会把自己注册进 database/sql，注册名是 "sqlite"——
	// 所以下面 sql.Open("sqlite", ...) 才找得到驱动。
	// 删掉这行，运行时会报 sql: unknown driver "sqlite"（编译期不会报错，因为语法上它是合法的）。
	_ "modernc.org/sqlite" // 纯 Go 实现的 SQLite，不需要 cgo / gcc / VS Build Tools

	"ar-inspection/server/internal/model"
)

// schema 是"表结构定义"和"启动时的建表动作"合二为一：只在 Open 里执行一次。
//
// 每一列与契约 6.2 一一对应，包括两处 CHECK：
// 它们是数据库层的兜底——就算哪天 handler 的校验被改坏，脏数据也进不了库
// （比如 status 写了 openn，这里直接拒绝，而不是存进去等前端显示成空白）。
//
// ⚠ 一条必须知道的限制：IF NOT EXISTS 的意思是"表已经存在就整条跳过"。
// 所以它不是迁移工具——将来给表加字段（比如截图 screenshot_url），
// 已有的数据库不会自动获得新列，程序会带着旧表跑，然后在查询时报 no such column。
// 加字段的正确做法：在 Open 里按版本号补几条 ALTER TABLE，或者删库重建（演示阶段可接受）。
// 这条缺口记在 docs/api.md 的"已知缺口"里。
const schema = `
CREATE TABLE IF NOT EXISTS inspections (
    id          TEXT PRIMARY KEY,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    priority    TEXT NOT NULL CHECK (priority IN ('low','medium','high')),
    status      TEXT NOT NULL CHECK (status IN ('open','in_progress','resolved')),
    x           REAL NOT NULL,
    y           REAL NOT NULL,
    z           REAL NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_inspections_created ON inspections(created_at DESC);
`

// Store 是数据库的句柄。
//
// 里面那个 *sql.DB 不是"一条连接"，而是**连接池**：可以并发用，内部自己排队。
// 真正的连接由它按需开、按需关，调用方不用管——这也是为什么整个服务只创建一个 Store。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）数据库并建表。进程重启后数据还在，靠的就是这个文件。
//
// 四步，顺序有意义：
//  1. 先确保目录存在——否则打开文件会失败，而报错信息会很含糊
//  2. 打开文件（不存在就创建）
//  3. 把连接池压到 1（原因见下）
//  4. 建表。放在这里而不是"第一次写入时"，是为了让启动阶段就把问题暴露出来：
//     数据库文件坏了、目录没权限，启动就该失败，而不是等到用户点提交时报错
func Open(path string) (*Store, error) {
	// ① 目录不存在就建。filepath.Dir("data/inspections.db") 得到 "data"；
	// 如果路径里没有目录部分（比如 "x.db"），Dir 返回 "."，那种情况跳过即可。
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据目录失败: %w", err)
		}
	}

	// ② 打开数据库。两个 pragma 一起出现不是随手配的，它们解决的是两个不同的问题：
	//   busy_timeout(5000)：别人正在写的时候，我最多等 5 秒而不是立刻报 database is locked。
	//     没有它，两个请求撞在一起时，后一个会当场失败——用户看到的是随机报错。
	//   journal_mode(WAL)：读写不互相阻塞。没它的话，一次写会挡住所有读，
	//     而管理端每 10 秒轮询一次 health、你同时在改状态，正好会撞上。
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// ③ SQLite 同一时刻只允许一个写者。这里把连接池压到 1，让并发请求在
	// **我们这一层**排队，比让它们到 SQLite 里互相撞 locked 更可控。
	// 代价是写操作串行——这个项目的数据量（一周几十条）完全不需要并发写。
	// 顺带一个好处：串行之后，读到的数据不会出现"写了一半"的中间态。
	db.SetMaxOpenConns(1)

	// ④ 建表。这里的 context 来自 Background 而不是某个请求——
	// 因为它发生在启动阶段，此时还没有任何 HTTP 请求。
	// 5 秒超时是为了防止"文件被别的进程锁住"时启动无限挂起。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		// 建表失败就把连接关掉再返回错误，不留半开的资源
		db.Close()
		return nil, fmt.Errorf("建表失败: %w", err)
	}
	return &Store{db: db}, nil
}

// Close 关闭连接池。main.go 里用 defer 调用，保证进程退出前落盘。
func (s *Store) Close() error { return s.db.Close() }

// Ping 验证「服务活着，而且数据库真的通」，供 GET /api/health 使用。
//
// 它故意做一次真实的查询（SELECT 1）而不是只看文件在不在：
// 数据库文件损坏、被锁死、路径失效，都只有真的查一次才暴露得出来。
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.db.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

// selectColumns 把十个列名定义**一次**，List / Get / Insert 共用。
//
// 这一行就是最朴素的"表结构描述"：加字段时改这里 + 改 schema，四个方法自动跟着走。
// 顺序很重要——下面 scanInspection 的 Scan 参数顺序必须和它逐一对齐（原因见那里）。
const selectColumns = `id, title, description, priority, status, x, y, z, created_at, updated_at`

// List 返回全部记录，按创建时间倒序（契约要求：服务端固定排序，前端不再排）。
//
// 排序带两个键：created_at DESC, id DESC。
// 只按 created_at 排是不够的——它的精度到秒，同一秒内建的多条记录之间顺序不确定，
// 每次查询可能不一样。id 里带毫秒时间戳，用它做第二排序键，顺序就稳定了。
//
// 返回空切片而不是 nil：这样 JSON 序列化出来是 []，不是 null。
// 对前端来说"后端说一条都没有"和"字段不存在"是两件事，不能混淆。
func (s *Store) List(ctx context.Context) ([]model.Inspection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+selectColumns+` FROM inspections ORDER BY created_at DESC, id DESC`)
	if err != nil {
		// 查询都没发出去（连接问题、SQL 写错），直接返回
		return nil, err
	}
	// defer rows.Close()：函数返回时关掉结果集。
	// 不关会一直占着连接，而这个池子只有一个连接——那等于把服务锁死。
	defer rows.Close()

	// 预分配 16 个位置只是小优化，超出会自动扩容，不影响正确性
	items := make([]model.Inspection, 0, 16)
	// rows.Next() 每次前进一行，到底返回 false
	for rows.Next() {
		rec, err := scanInspection(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, rec)
	}
	// 遍历结束后必须再查一次 rows.Err()：中途出错（比如连接断了）是停在这里报的，
	// 而不是在 Next() 里。漏掉这一查，读到一半失败会被当成"就这么多数据"。
	return items, rows.Err()
}

// Get 按 id 取一条记录。
//
// 三个返回值：记录、是否存在、是否出错。
// "不存在"被刻意做成**正常结果**而不是错误——因为上层要区分两种情况：
//   - 记录不存在 → 404，客户端拿了个过期的 id（用户该刷新）
//   - 查库失败   → 500，我们的问题（用户重试也没用）
//
// 如果把不存在塞进 error，这两个分支在上层就分不开了。
func (s *Store) Get(ctx context.Context, id string) (model.Inspection, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+selectColumns+` FROM inspections WHERE id = ?`, id)
	rec, err := scanInspection(row)
	if err == sql.ErrNoRows {
		// sql.ErrNoRows 是标准库的哨兵错误：查询结果零行时 Scan 会返回它
		return model.Inspection{}, false, nil
	}
	if err != nil {
		return model.Inspection{}, false, err
	}
	return rec, true, nil
}

// Insert 写入一条记录。传进来的 rec 一定已经过 model 的校验。
//
// ⚠ 十个参数必须与 selectColumns 的列顺序严格对应：
//
//	1 id → 2 title → 3 description → 4 priority → 5 status
//	→ 6 x → 7 y → 8 z → 9 created_at → 10 updated_at
//
// 这里的坑很具体：如果把 6/7/8（三个坐标）写反了，编译照样通过、
// 运行时也不报错，只是把点放到了错误的位置——这种错误只能靠冒烟测试抓。
func (s *Store) Insert(ctx context.Context, rec model.Inspection) error {
	// 第一个返回值是 sql.Result（受影响行数等信息），插入不需要它，用 _ 丢掉
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO inspections (`+selectColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		// ? 是占位符，值由驱动做转义——这是防 SQL 注入的正解，
		// 千万不要用字符串拼接把用户输入拼进 SQL
		rec.ID, rec.Title, rec.Description, string(rec.Priority), string(rec.Status),
		rec.Position.X, rec.Position.Y, rec.Position.Z, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// UpdateStatus 只改 status 和 updated_at 两列，其它字段一概不动。
//
// 这就是 PATCH 的语义在存储层的落地：不是"整条替换"，而是"改这几列"。
// 也是为什么这个函数签名里只有 (id, status, updatedAt) —— 没有 title、没有 priority，
// 想改别的字段得另外加函数，而不是"顺手"在这里多传几个参数。
//
// 返回更新后的整条记录：因为接口要回 200 + 完整对象，而且管理端靠这条响应
// 覆盖本地那一行（它不自己拼状态）。
func (s *Store) UpdateStatus(ctx context.Context, id string, status model.Status, updatedAt string) (model.Inspection, bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE inspections SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), updatedAt, id)
	if err != nil {
		return model.Inspection{}, false, err
	}

	// 这里必须看受影响行数：UPDATE 一行都没匹配上时，SQL 层面仍然是"执行成功"，
	// 只有 RowsAffected() 会告诉你 0 —— 那意味着这条记录不存在，而不是"更新失败"。
	n, err := res.RowsAffected()
	if err != nil {
		return model.Inspection{}, false, err
	}
	if n == 0 {
		return model.Inspection{}, false, nil
	}

	// 再把刚写进去的整条读回来返回。多一次查询，但省掉了"本地拼一个对象"
	// 这种会和数据库不一致的做法。
	return s.Get(ctx, id)
}

// scanner 是一个只有 Scan 方法的接口。
//
// 作用是让 scanInspection 同时接受 *sql.Row（查询单行）和 *sql.Rows（查询多行）——
// 这两种类型都有 Scan，但签名一致、类型不同。定一个小接口，两边都能传进来，
// 扫描代码就只写一份，不会出现"单行版改了、多行版忘了改"的问题。
type scanner interface {
	Scan(dest ...any) error
}

// scanInspection 把数据库的一行装进 model.Inspection。
//
// ⚠ Scan 的参数顺序必须和 selectColumns 的列顺序逐一对齐，差一位就会把
// created_at 装进 title 这种荒谬的结果——而且不会报错。
// 这是 SQL + Go 组合里最容易犯又最难发现的错，所以两边都加了这句提醒。
//
// 为什么 priority / status 先扫进 string 再转换：数据库里存的是文本，
// 但代码里想要枚举类型。先扫进临时变量、再显式转换，读的人一眼能看出这里做了类型转换。
func scanInspection(sc scanner) (model.Inspection, error) {
	var (
		rec      model.Inspection
		priority string
		status   string
	)
	err := sc.Scan(&rec.ID, &rec.Title, &rec.Description, &priority, &status,
		&rec.Position.X, &rec.Position.Y, &rec.Position.Z, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		return model.Inspection{}, err
	}
	rec.Priority = model.Priority(priority)
	rec.Status = model.Status(status)
	return rec, nil
}
