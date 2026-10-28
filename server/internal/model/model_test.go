package model

import (
	"math"
	"strings"
	"testing"
	"time"
)

// TestDraftValidate 逐条对应契约 6.2 的约束与 6.3 的四个错误码示例。
// 表驱动：加一条规则就加一行，不用改测试结构。
func TestDraftValidate(t *testing.T) {
	valid := func() Draft {
		return Draft{
			Title:       "B1 入口墙面破损",
			Description: "左侧墙体存在裂缝",
			Priority:    "high",
			Position:    Position{X: 0.42, Y: -1.30, Z: 1.26},
			HasPosition: true,
		}
	}

	cases := []struct {
		name    string
		mutate  func(*Draft)
		wantMsg string // 空串 = 期望通过
	}{
		{name: "合法", mutate: func(d *Draft) {}, wantMsg: ""},
		{name: "标题为空", mutate: func(d *Draft) { d.Title = "" }, wantMsg: "标题不能为空"},
		{name: "标题全是空格", mutate: func(d *Draft) { d.Title = "   " }, wantMsg: "标题不能为空"},
		{name: "标题超长", mutate: func(d *Draft) { d.Title = strings.Repeat("裂", 101) }, wantMsg: "标题最多 100 字"},
		{name: "标题正好 100 字", mutate: func(d *Draft) { d.Title = strings.Repeat("裂", 100) }, wantMsg: ""},
		{name: "描述超长", mutate: func(d *Draft) { d.Description = strings.Repeat("水", 501) }, wantMsg: "描述最多 500 字"},
		{name: "描述可空", mutate: func(d *Draft) { d.Description = "" }, wantMsg: ""},
		{name: "优先级不认识", mutate: func(d *Draft) { d.Priority = "urgent" }, wantMsg: "优先级必须是 low、medium 或 high"},
		{name: "优先级缺失", mutate: func(d *Draft) { d.Priority = "" }, wantMsg: "优先级必须是 low、medium 或 high"},
		{name: "缺坐标", mutate: func(d *Draft) { d.HasPosition = false }, wantMsg: "缺少位置坐标"},
		{name: "x 是 NaN", mutate: func(d *Draft) { d.Position.X = math.NaN() }, wantMsg: "坐标不是有效数字"},
		{name: "y 是 +Inf", mutate: func(d *Draft) { d.Position.Y = math.Inf(1) }, wantMsg: "坐标不是有效数字"},
		{name: "y 是负数合法", mutate: func(d *Draft) { d.Position.Y = -1.5 }, wantMsg: ""},
		{name: "标题首尾空格被接受", mutate: func(d *Draft) { d.Title = "  B1 墙面裂缝  " }, wantMsg: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := valid()
			tc.mutate(&d)
			err := d.Validate()

			if tc.wantMsg == "" {
				if err != nil {
					t.Fatalf("期望通过，却报错: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("期望报错 %q，实际通过", tc.wantMsg)
			}
			if err.Code != CodeInvalidArgument {
				t.Errorf("错误码 = %s，期望 %s", err.Code, CodeInvalidArgument)
			}
			if err.Message != tc.wantMsg {
				t.Errorf("错误信息 = %q，期望 %q", err.Message, tc.wantMsg)
			}
		})
	}
}

// TestCanTransition 把契约 6.6 的状态机逐格钉死，包括本轮明确允许的跳级。
func TestCanTransition(t *testing.T) {
	all := []Status{StatusOpen, StatusInProgress, StatusResolved}

	// 期望表：以「从 → 到」为键
	want := map[[2]Status]bool{
		{StatusOpen, StatusOpen}:             true,  // 幂等
		{StatusOpen, StatusInProgress}:       true,  // 前进
		{StatusOpen, StatusResolved}:         true,  // 前进 + 跳级（本轮确认允许）
		{StatusInProgress, StatusOpen}:       false, // 回退，409
		{StatusInProgress, StatusInProgress}: true,  // 幂等
		{StatusInProgress, StatusResolved}:   true,  // 前进
		{StatusResolved, StatusOpen}:         true,  // 唯一允许的回退（复发，BR-07）
		{StatusResolved, StatusInProgress}:   false, // 回退，409
		{StatusResolved, StatusResolved}:     true,  // 幂等
	}

	for _, from := range all {
		for _, to := range all {
			got := CanTransition(from, to)
			if got != want[[2]Status{from, to}] {
				t.Errorf("%s → %s = %v，期望 %v", from, to, got, want[[2]Status{from, to}])
			}
		}
	}
}

func TestTransitionError(t *testing.T) {
	if e := TransitionError(StatusResolved, StatusInProgress); e.Code != CodeInvalidTransition || e.Message != "已解决的问题不能重新处理" {
		t.Errorf("resolved→in_progress 的提示语不对: %v", e)
	}
	if e := TransitionError(StatusInProgress, StatusOpen); e.Message != "不能从「处理中」改成「待处理」" {
		t.Errorf("通用提示语不对: %q", e.Message)
	}
}

// TestNewID 钉住 id 的形状：前缀、长度、字符集、同一毫秒内不重复、时间有序。
func TestNewID(t *testing.T) {
	now := time.Date(2026, 10, 27, 13, 21, 0, 0, time.UTC)
	a := NewID(now)

	if !strings.HasPrefix(a, "insp_") {
		t.Fatalf("缺少前缀: %s", a)
	}
	if len(a) != len("insp_")+26 {
		t.Fatalf("长度 = %d，期望 %d", len(a), len("insp_")+26)
	}
	for _, c := range a {
		if !strings.ContainsRune(crockfordBase32+"insp_", c) {
			t.Fatalf("出现了非法字符 %q: %s", c, a)
		}
	}

	seen := map[string]bool{a: true}
	for i := 0; i < 500; i++ {
		id := NewID(now)
		if seen[id] {
			t.Fatalf("同一毫秒内产生了重复 id: %s", id)
		}
		seen[id] = true
	}

	// 时间戳段（前 10 位）应随毫秒单调不减
	later := NewID(now.Add(time.Millisecond))
	if later[:len("insp_")+10] < a[:len("insp_")+10] {
		t.Errorf("id 时间前缀没有随时间前进: %s vs %s", a, later)
	}
}

func TestPositionRounded(t *testing.T) {
	p := Position{X: 0.4212345, Y: -1.3005, Z: 1.0}
	r := p.Rounded()
	if r.X != 0.421 || r.Y != -1.301 || r.Z != 1.0 {
		t.Errorf("三位小数收敛不对: %+v", r)
	}
}
