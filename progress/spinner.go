package progress

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// 管理旋转动画的状态，支持动态消息更新。
type Spinner struct {
	message      atomic.Value // 原子操作的动态消息（线程安全）
	messageWidth int          // 消息固定宽度（用于对齐）

	parts []string // 旋转动画帧（Unicode字符序列）

	value int // 当前动画帧索引

	ticker  *time.Ticker // 定时器（控制动画更新频率）
	started time.Time    // 动画启动时间
	stopped time.Time    // 动画停止时间
}

// 使用 Unicode 字符实现旋转动画。
// 启动后台协程 (start()) 每 100ms 更新动画帧。
func NewSpinner(message string) *Spinner {
	s := &Spinner{
		parts: []string{
			"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
		},
		started: time.Now(), // 记录启动时间
	}
	s.SetMessage(message) // 初始化消息
	go s.start()          // 启动动画更新循环
	return s
}

// 线程安全地更新显示的消息。
func (s *Spinner) SetMessage(message string) {
	s.message.Store(message) // 原子操作更新消息
}

// 消息截断对齐（若 messageWidth 被设置）。
// 根据当前动画帧索引显示旋转字符。
func (s *Spinner) String() string {
	var sb strings.Builder
	// 1. 处理消息
	if message, ok := s.message.Load().(string); ok && len(message) > 0 {
		message := strings.TrimSpace(message)
		if s.messageWidth > 0 && len(message) > s.messageWidth {
			message = message[:s.messageWidth] // 截断消息
		}

		fmt.Fprintf(&sb, "%s", message)
		if padding := s.messageWidth - sb.Len(); padding > 0 {
			sb.WriteString(strings.Repeat(" ", padding)) // 填充空格对齐
		}

		sb.WriteString(" ")
	}
	// 2. 添加旋转动画
	if s.stopped.IsZero() { // 未停止时显示动画
		spinner := s.parts[s.value]
		sb.WriteString(spinner)
		sb.WriteString(" ")
	}

	return sb.String()
}

// 驱动动画帧的循环切换。
func (s *Spinner) start() {
	s.ticker = time.NewTicker(100 * time.Millisecond) // 每100ms触发一次
	for range s.ticker.C {
		s.value = (s.value + 1) % len(s.parts) // 循环切换动画帧
		if !s.stopped.IsZero() {               // 若已停止，退出循环
			return
		}
	}
}

// 停止动画更新（通过设置 stopped 时间）
func (s *Spinner) Stop() {
	if s.stopped.IsZero() {
		s.stopped = time.Now() // 标记停止时间
	}
}
