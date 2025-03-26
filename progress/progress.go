package progress

import (
	"bufio"
	"fmt"
	"io"
	"sync"
	"time"
)

// 多行终端进度管理器，支持动态添加状态（如进度条、旋转动画），定时刷新显示。

type State interface {
	String() string // 所有状态需实现此方法（如 Spinner、Bar）
}

type Progress struct {
	mu sync.Mutex // 互斥锁，保护共享数据
	// buffer output to minimize flickering on all terminals
	w *bufio.Writer // 缓冲写入器（减少终端闪烁）

	pos int // 当前光标位置（用于多行渲染）

	ticker *time.Ticker // 定时器（控制刷新频率）
	states []State      // 存储所有状态（如 Spinner、Bar）
}

// 创建进度管理器，启动定时刷新（默认100ms间隔）
func NewProgress(w io.Writer) *Progress {
	p := &Progress{w: bufio.NewWriter(w)} // 初始化缓冲写入器
	go p.start()                          // 启动后台定时刷新
	return p
}

func (p *Progress) stop() bool {
	// 停止所有 Spinner 状态
	for _, state := range p.states {
		if spinner, ok := state.(*Spinner); ok {
			spinner.Stop()
		}
	}

	if p.ticker != nil {
		p.ticker.Stop() // 停止定时器
		p.ticker = nil
		p.render() // 最后一次渲染
		return true
	}

	return false
}

// 保留最终状态，换行
func (p *Progress) Stop() bool {
	stopped := p.stop()
	if stopped {
		fmt.Fprint(p.w, "\n") // 输出换行符
		p.w.Flush()           // 确保内容写入终端
	}
	return stopped
}

// 完全清除所有进度行。
func (p *Progress) StopAndClear() bool {
	defer p.w.Flush()
	// 隐藏光标
	fmt.Fprint(p.w, "\033[?25l")
	defer fmt.Fprint(p.w, "\033[?25h")

	stopped := p.stop()
	if stopped {
		// 清除所有进度行
		// clear all progress lines
		for i := range p.pos {
			if i > 0 {
				fmt.Fprint(p.w, "\033[A") // 光标上移
			}
			fmt.Fprint(p.w, "\033[2K\033[1G") // 清行并复位光标
		}
	}

	return stopped
}

// 向进度管理器添加一个状态（如进度条或旋转动画）
func (p *Progress) Add(key string, state State) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.states = append(p.states, state)
}

// 使用 ANSI 控制码控制光标位置和同步输出。
// 逐行渲染所有状态，确保终端显示不闪烁。
func (p *Progress) render() {
	p.mu.Lock()
	defer p.mu.Unlock()

	defer p.w.Flush() // 确保缓冲内容写入终端
	// ANSI 控制码：启用同步输出（减少闪烁）
	// eliminate flickering on terminals that support synchronized output
	fmt.Fprint(p.w, "\033[?2026h")
	defer fmt.Fprint(p.w, "\033[?2026l")
	// 隐藏光标
	fmt.Fprint(p.w, "\033[?25l")
	defer fmt.Fprint(p.w, "\033[?25h")
	// 光标复位：移动到第一行首列
	// move the cursor back to the beginning
	for range p.pos - 1 {
		fmt.Fprint(p.w, "\033[A") // 上移光标
	}
	fmt.Fprint(p.w, "\033[1G") // 移动到行首
	// 渲染所有状态
	// render progress lines
	for i, state := range p.states {
		fmt.Fprint(p.w, state.String(), "\033[K") // 渲染状态并清空行尾
		if i < len(p.states)-1 {
			fmt.Fprint(p.w, "\n") // 换行（多行进度）
		}
	}

	p.pos = len(p.states) // 记录当前行数
}

// 定时调用 render 方法更新终端显示
func (p *Progress) start() {
	p.ticker = time.NewTicker(100 * time.Millisecond)
	for range p.ticker.C {
		p.render()
	}
}
