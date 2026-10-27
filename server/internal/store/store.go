// Package store 拥有全部 SQL 知识。它不认识 HTTP、不认识 JSON、不认识状态码，
// 只知道一张表和三四个动作。想换存储（比如换成 Postgres），改这一个包就够。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // 纯 Go 实现的 SQLite，不需要 cgo / gcc / VS Build Tools

	"ar-inspection/server/internal/model"
)

// schema 与契约 6.2 一一对应。
// CHECK 约束是数据库层的兜底：就算哪天 handler 的校验被改坏，脏数据也进不了库。
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

type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）数据库并建表。进程重启后数据仍在，靠的就是这个文件。
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据目录失败: %w", err)
		}
	}

	// _pragma 是 modernc.org/sqlite 的 DSN 写法：
	//   busy_timeout 别人在写时最多等 5 秒而不是立刻报 locked
	//   journal_mode=WAL 读写不互相阻塞，演示时浏览器轮询和写入不会打架
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// SQLite 只允许一个写者。把连接池压到 1，让并发请求串行排队，
	// 比让它们互相撞 locked 更省心——这个项目的数据量完全不需要并发写。
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("建表失败: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Ping 供 /api/health 用：验证「服务活着，而且数据库真的通」。
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.db.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

const selectColumns = `id, title, description, priority, status, x, y, z, created_at, updated_at`

// List 按创建时间倒序返回全部记录（契约 6.1：服务端固定排序，前端不再排）。
// 返回空切片而不是 nil，这样 JSON 里是 [] 而不是 null。
func (s *Store) List(ctx context.Context) ([]model.Inspection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+selectColumns+` FROM inspections ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Inspection, 0, 16)
	for rows.Next() {
		rec, err := scanInspection(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, rec)
	}
	return items, rows.Err()
}

// Get 按 id 取一条。第二个返回值表示「有没有」——区分「不存在」和「查库出错」。
func (s *Store) Get(ctx context.Context, id string) (model.Inspection, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+selectColumns+` FROM inspections WHERE id = ?`, id)
	rec, err := scanInspection(row)
	if err == sql.ErrNoRows {
		return model.Inspection{}, false, nil
	}
	if err != nil {
		return model.Inspection{}, false, err
	}
	return rec, true, nil
}

// Insert 写入一条已经由 model 校验过的记录。
func (s *Store) Insert(ctx context.Context, rec model.Inspection) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO inspections (`+selectColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.Title, rec.Description, string(rec.Priority), string(rec.Status),
		rec.Position.X, rec.Position.Y, rec.Position.Z, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// UpdateStatus 只改状态与 updatedAt，返回更新后的整条（契约 6.1：PATCH 返回整个对象）。
func (s *Store) UpdateStatus(ctx context.Context, id string, status model.Status, updatedAt string) (model.Inspection, bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE inspections SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), updatedAt, id)
	if err != nil {
		return model.Inspection{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.Inspection{}, false, err
	}
	if n == 0 {
		return model.Inspection{}, false, nil
	}
	return s.Get(ctx, id)
}

// scanner 让 *sql.Row 和 *sql.Rows 共用同一段扫描代码。
type scanner interface {
	Scan(dest ...any) error
}

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
