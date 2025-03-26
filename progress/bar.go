package progress

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/bnulwh/ollama/format"
)

// 动态终端进度条，支持消息、百分比、进度图形、速率、剩余时间

// 存储进度条的状态和配置。
type Bar struct {
	message      string // 进度条显示的消息
	messageWidth int    // 消息固定宽度（用于对齐）

	maxValue     int64 // 最大值（如总字节数）
	initialValue int64 // 初始值
	currentValue int64 // 当前值

	started time.Time // 进度条启动时间
	stopped time.Time // 进度条完成时间 若进度完成，记录完成时间。

	maxBuckets int      // 速率计算的时间窗口大小
	buckets    []bucket // 记录时间点的值（用于计算速率）
}

// 存储某一时刻的进度值，用于计算速率。
type bucket struct {
	updated time.Time // 记录时间点
	value   int64     // 对应的值
}

// 初始化进度条，若初始值已满足最大值，直接标记为完成。
func NewBar(message string, maxValue, initialValue int64) *Bar {
	b := Bar{
		message:      message,
		messageWidth: -1,
		maxValue:     maxValue,
		initialValue: initialValue,
		currentValue: initialValue,
		started:      time.Now(),
		maxBuckets:   10, // 默认保留10个时间点
	}

	if initialValue >= maxValue {
		b.stopped = time.Now() // 初始值已满，直接完成
	}

	return &b
}

// 将时间间隔格式化为易读形式。
// formatDuration limits the rendering of a time.Duration to 2 units
func formatDuration(d time.Duration) string {
	switch {
	case d >= 100*time.Hour:
		return "99h+" // 超过100小时显示99h+
	case d >= time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return d.Round(time.Second).String()
	}
}

func (b *Bar) String() string {
	termWidth, _, err := term.GetSize(int(os.Stderr.Fd())) // 获取终端宽度
	if err != nil {
		termWidth = 80 // 默认80列
	}
	// 1. 构建前缀（消息 + 百分比）
	var pre strings.Builder
	if len(b.message) > 0 {
		message := strings.TrimSpace(b.message)
		if b.messageWidth > 0 && len(message) > b.messageWidth {
			message = message[:b.messageWidth] // 截断消息
		}

		fmt.Fprintf(&pre, "%s", message)
		if padding := b.messageWidth - pre.Len(); padding > 0 {
			pre.WriteString(repeat(" ", padding)) // 填充空格对齐
		}

		pre.WriteString(" ")
	}

	fmt.Fprintf(&pre, "%3.0f%%", b.percent()) // 显示百分比（如 42%）
	// 2. 构建后缀（当前值/最大值、速率、剩余时间）
	var suf strings.Builder
	// max 13 characters: "999 MB/999 MB"
	if b.stopped.IsZero() { // 未完成
		curValue := format.HumanBytes(b.currentValue)
		suf.WriteString(repeat(" ", 6-len(curValue)))
		suf.WriteString(curValue)
		suf.WriteString("/")

		maxValue := format.HumanBytes(b.maxValue)
		suf.WriteString(repeat(" ", 6-len(maxValue)))
		suf.WriteString(maxValue)
	} else {
		// 已完成
		maxValue := format.HumanBytes(b.maxValue)
		suf.WriteString(repeat(" ", 6-len(maxValue)))
		suf.WriteString(maxValue)
		suf.WriteString(repeat(" ", 7))
	}
	// 动态速率
	rate := b.rate()
	// max 10 characters: "  999 MB/s"
	if b.stopped.IsZero() && rate > 0 {
		suf.WriteString("  ")
		humanRate := format.HumanBytes(int64(rate))
		suf.WriteString(repeat(" ", 6-len(humanRate)))
		suf.WriteString(humanRate)
		suf.WriteString("/s")
	} else {
		suf.WriteString(repeat(" ", 10))
	}

	// max 8 characters: "  59m59s"
	if b.stopped.IsZero() && rate > 0 {
		// 剩余时间
		suf.WriteString("  ")
		var remaining time.Duration
		if rate > 0 {
			remaining = time.Duration(int64(float64(b.maxValue-b.currentValue)/rate)) * time.Second
		}

		humanRemaining := formatDuration(remaining)
		suf.WriteString(repeat(" ", 6-len(humanRemaining)))
		suf.WriteString(humanRemaining)
	} else {
		suf.WriteString(repeat(" ", 8))
	}
	// 3. 构建进度条图形
	var mid strings.Builder
	// add 5 extra spaces: 2 boundary characters and 1 space at each end
	f := termWidth - pre.Len() - suf.Len() - 5
	n := int(float64(f) * b.percent() / 100)

	mid.WriteString(" ▕")

	if n > 0 {
		mid.WriteString(repeat("█", n))
	}

	if f-n > 0 {
		mid.WriteString(repeat(" ", f-n))
	}

	mid.WriteString("▏ ")
	// 3. 构建进度条图形
	return pre.String() + mid.String() + suf.String()
}

// 更新进度值，并记录时间点用于速率计算。
func (b *Bar) Set(value int64) {
	if value >= b.maxValue {
		value = b.maxValue
	}

	b.currentValue = value
	if b.currentValue >= b.maxValue {
		b.stopped = time.Now() // 标记完成
	}
	// 限制每秒最多记录一个时间点
	// throttle bucket updates to 1 per second
	if len(b.buckets) == 0 || time.Since(b.buckets[len(b.buckets)-1].updated) > time.Second {
		b.buckets = append(b.buckets, bucket{
			updated: time.Now(),
			value:   value,
		})

		if len(b.buckets) > b.maxBuckets {
			b.buckets = b.buckets[1:] // 滑动窗口，保留最近10个点
		}
	}
}

func (b *Bar) percent() float64 {
	if b.maxValue > 0 {
		return float64(b.currentValue) / float64(b.maxValue) * 100
	}

	return 0
}

// 若已完成：使用总时间计算平均速率。
// 若未完成：使用时间窗口（buckets）计算动态速率。
func (b *Bar) rate() float64 {
	var numerator, denominator float64

	if !b.stopped.IsZero() { // 已完成：总速率
		numerator = float64(b.currentValue - b.initialValue)
		denominator = b.stopped.Sub(b.started).Round(time.Second).Seconds()
	} else {
		// 根据时间窗口计算平均速率
		switch len(b.buckets) {
		case 0:
			// noop
		case 1:
			numerator = float64(b.buckets[0].value - b.initialValue)
			denominator = b.buckets[0].updated.Sub(b.started).Round(time.Second).Seconds()
		default:
			first, last := b.buckets[0], b.buckets[len(b.buckets)-1]
			numerator = float64(last.value - first.value)
			denominator = last.updated.Sub(first.updated).Round(time.Second).Seconds()
		}
	}

	if denominator != 0 {
		return numerator / denominator
	}

	return 0
}

func repeat(s string, n int) string {
	if n > 0 {
		return strings.Repeat(s, n)
	}

	return ""
}
