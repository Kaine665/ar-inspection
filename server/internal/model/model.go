// Package model 拥有这个系统里全部「业务知识」：一条记录长什么样、什么算合法、
// 状态能怎么变。它不碰数据库、不碰 HTTP、不 import 项目里任何其他包。
// 这样做的好处很具体：整个包是纯函数，可以单测，而且改校验规则时不用碰别的东西。
package model

import (
	"crypto/rand"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------
// 枚举（契约 6.2）
// ---------------------------------------------------------------

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
)

type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusResolved   Status = "resolved"
)

// 字段长度上限（契约 6.2）
const (
	MaxTitleLen       = 100
	MaxDescriptionLen = 500
)

func ParsePriority(s string) (Priority, bool) {
	switch Priority(s) {
	case PriorityLow, PriorityMedium, PriorityHigh:
		return Priority(s), true
	}
	return "", false
}

func ParseStatus(s string) (Status, bool) {
	switch Status(s) {
	case StatusOpen, StatusInProgress, StatusResolved:
		return Status(s), true
	}
	return "", false
}

// StatusLabel 只用于给人类看的提示语，不参与存储。
func StatusLabel(s Status) string {
	switch s {
	case StatusOpen:
		return "待处理"
	case StatusInProgress:
		return "处理中"
	case StatusResolved:
		return "已解决"
	}
	return string(s)
}

// ---------------------------------------------------------------
// 记录（契约 6.2 的逐字段翻译）
// ---------------------------------------------------------------

// Position 是现场坐标，单位米。y 通常是负数——原点在手机启动时的高度，
// 地面在下方约 1.0~1.5 米。这不是 bug。
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Inspection 就是 GET /api/inspections 里 items 的每一个元素。
// json tag 必须和契约一致，三端都按这个名字解析。
type Inspection struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    Priority `json:"priority"`
	Status      Status   `json:"status"`
	Position    Position `json:"position"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

// ---------------------------------------------------------------
// 错误（契约 6.3）
//
// 这里只表达「哪一类错、给人看的话是什么」，不含 HTTP 状态码——
// 那是 HTTP 层的事，由 api 包负责映射。model 因此完全不依赖 HTTP。
// ---------------------------------------------------------------

type Code string

const (
	CodeInvalidArgument   Code = "invalid_argument"
	CodeNotFound          Code = "not_found"
	CodeInvalidTransition Code = "invalid_transition"
	CodeInternal          Code = "internal"
)

type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

func InvalidArgument(format string, args ...any) *Error {
	return &Error{Code: CodeInvalidArgument, Message: fmt.Sprintf(format, args...)}
}

func NotFound(format string, args ...any) *Error {
	return &Error{Code: CodeNotFound, Message: fmt.Sprintf(format, args...)}
}

func InvalidTransition(format string, args ...any) *Error {
	return &Error{Code: CodeInvalidTransition, Message: fmt.Sprintf(format, args...)}
}

func Internal(format string, args ...any) *Error {
	return &Error{Code: CodeInternal, Message: fmt.Sprintf(format, args...)}
}

// ---------------------------------------------------------------
// 校验（契约 6.2 约束列 + 冻结点 3「只返回第一个错误」）
//
// 顺序是**固定**的，并写进文档：标题 → 描述长度 → 优先级 → 坐标存在 → 坐标有限。
// 固定顺序的意义：同一个坏请求永远得到同一个错误，不会因为实现顺序变化而漂。
// ---------------------------------------------------------------

// Draft 是「手机端提交上来、还没落库的一条记录」。
// 用独立结构体而不是 Inspection：id / status / 时间戳这些只有服务端能生成的字段，
// 在这里压根没有位置可放，客户端传了也进不来。
type Draft struct {
	Title       string
	Description string
	Priority    string
	Position    Position
	HasPosition bool
}

func (d Draft) Validate() *Error {
	title := strings.TrimSpace(d.Title)
	if title == "" {
		return InvalidArgument("标题不能为空")
	}
	if utf8.RuneCountInString(title) > MaxTitleLen {
		return InvalidArgument("标题最多 %d 字", MaxTitleLen)
	}
	if utf8.RuneCountInString(d.Description) > MaxDescriptionLen {
		return InvalidArgument("描述最多 %d 字", MaxDescriptionLen)
	}
	if _, ok := ParsePriority(d.Priority); !ok {
		return InvalidArgument("优先级必须是 low、medium 或 high")
	}
	if !d.HasPosition {
		return InvalidArgument("缺少位置坐标")
	}
	if !isFinite(d.Position.X) || !isFinite(d.Position.Y) || !isFinite(d.Position.Z) {
		return InvalidArgument("坐标不是有效数字")
	}
	return nil
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// Rounded 把坐标收敛到 3 位小数（契约 6.2 的约束）。
// 在服务端统一收口，而不是信任客户端：Unity 传什么精度是它的事，
// 库里存的必须是同一个口径，否则不同设备的同一点会显示成两个地方。
func (p Position) Rounded() Position {
	return Position{
		X: math.Round(p.X*1000) / 1000,
		Y: math.Round(p.Y*1000) / 1000,
		Z: math.Round(p.Z*1000) / 1000,
	}
}

// NormalizeTitle 去掉首尾空白，落库的是干净版本。
func (d Draft) NormalizeTitle() string { return strings.TrimSpace(d.Title) }

// ---------------------------------------------------------------
// 状态机（契约 6.6 冻结点 1&2 + 本轮确认）
//
//   open → in_progress   ✓ 前进
//   in_progress → resolved  ✓ 前进
//   open → resolved      ✓ 前进，且允许**跳级**（现场自己好了 / 误报，一步关掉）
//   resolved → open      ✓ 唯一允许的回退（裂缝会复发，BR-07）
//   in_progress → open   ✗ 409（回退会抹掉「曾有进展」这个事实）
//   resolved → in_progress ✗ 409
//   任意 → 同一个值      ✓ 幂等，200，且**不刷新时间戳**
// ---------------------------------------------------------------

func CanTransition(from, to Status) bool {
	if from == to {
		return true
	}
	switch to {
	case StatusResolved:
		// 向前一律允许，包含从 open 跳级
		return true
	case StatusInProgress:
		return from == StatusOpen
	case StatusOpen:
		return from == StatusResolved
	}
	return false
}

// TransitionError 生成 409 的中文说明。挑一条最贴近现场的句子，而不是模板腔。
func TransitionError(from, to Status) *Error {
	if from == StatusResolved && to == StatusInProgress {
		return InvalidTransition("已解决的问题不能重新处理")
	}
	return InvalidTransition("不能从「%s」改成「%s」", StatusLabel(from), StatusLabel(to))
}

// ---------------------------------------------------------------
// ID 生成：insp_ + ULID
//
// 为什么用 ULID 而不是自增整数：
//   - 契约要求「为新问题生成唯一 ID」，ULID 自带 48 位毫秒时间戳，天然按时间有序；
//   - 不依赖数据库自增，换存储不用改语义；
//   - 26 个字符、URL 安全，肉眼可读，日志里能直接贴。
// 契约说 id 是「不透明字符串，前端只透传不解析」，所以格式由服务端自由决定。
// ---------------------------------------------------------------

const crockfordBase32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewID 生成形如 insp_01JBX3Z9Q4F5K7M2N8P1R6S0TV 的 id（10 位时间 + 16 位随机）。
func NewID(t time.Time) string {
	var raw [16]byte
	ms := uint64(t.UnixMilli())
	raw[0] = byte(ms >> 40)
	raw[1] = byte(ms >> 32)
	raw[2] = byte(ms >> 24)
	raw[3] = byte(ms >> 16)
	raw[4] = byte(ms >> 8)
	raw[5] = byte(ms)
	if _, err := rand.Read(raw[6:]); err != nil {
		// crypto/rand 失败在现代系统上基本等于进程有问题，退化成时间戳也能保证唯一
		for i := 6; i < 16; i++ {
			raw[i] = byte(ms>>uint(i%8)) ^ byte(t.Nanosecond()>>uint(i%8))
		}
	}

	buf := make([]byte, 0, 32)
	buf = append(buf, "insp_"...)
	buf = appendBase32(buf, raw[0:6], 10)  // 48 位时间 → 10 字符
	buf = appendBase32(buf, raw[6:16], 16) // 80 位随机 → 16 字符
	return string(buf)
}

// appendBase32 从 src 的比特流里按 5 位一组取字符，结果补齐/截断到 width 个字符。
// 48 位 → 9 个满字符 + 3 位余数 → 第 10 个字符；80 位 → 正好 16 个字符。
func appendBase32(dst []byte, src []byte, width int) []byte {
	out := make([]byte, 0, width+1)
	var acc uint32
	var bits uint
	for _, b := range src {
		acc = acc<<8 | uint32(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, crockfordBase32[(acc>>bits)&0x1F])
		}
	}
	if bits > 0 {
		out = append(out, crockfordBase32[(acc<<(5-bits))&0x1F])
	}
	// 不足 width 时左补 '0'（等价于高位补零，ULID 的时间戳段就是这样）
	for len(out) < width {
		out = append([]byte{crockfordBase32[0]}, out...)
	}
	if len(out) > width {
		out = out[len(out)-width:]
	}
	return append(dst, out...)
}

// Now 统一时间口径：UTC + RFC3339（契约 6.2 的 createdAt 格式）。
func Now() time.Time { return time.Now().UTC() }

func FormatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }
