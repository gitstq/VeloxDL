// Package progress 负责把下载进度渲染成人类可读的单行输出。
package progress

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gitstq/VeloxDL/internal/engine"
)

// barWidth 是进度条字符宽度。
const barWidth = 28

// Render 把快照渲染成一行并写入 w。
//
// 输出形如：
//
//	[████████████░░░░░░░░░░░░] 42.9%  86.0 MB / 200.4 MB   11.2 MB/s  eta 10s  conns 8/16
//
// 行首用 \r 回车、\033[K 清行，实现原地刷新而不刷屏。
func Render(w io.Writer, s engine.Snapshot) {
	pct := s.Percent()
	if pct > 100 {
		pct = 100
	}

	filled := int(pct / 100 * barWidth)
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	var eta string
	if remain := s.ETA(); remain > 0 && pct < 100 {
		eta = fmt.Sprintf("  eta %s", formatDuration(remain))
	}

	fmt.Fprintf(w, "\r\033[K[%s] %5.1f%%  %s / %s   %s/s%s  conns %d",
		bar,
		pct,
		HumanBytes(s.Saved),
		HumanBytes(s.Total),
		HumanBytes(int64(s.Rate)),
		eta,
		s.Conns,
	)
}

// Done 输出下载完成后的汇总行（清掉进度条所在行后换行）。
func Done(w io.Writer, s engine.Snapshot) {
	fmt.Fprintf(w, "\r\033[K✅ 完成 %s  %s  %s  平均 %s/s  用时 %s  并发 %d  模式 %s\n",
		s.Output,
		HumanBytes(s.Saved),
		"100.0%",
		HumanBytes(int64(s.Overall)),
		formatDuration(s.Elapsed),
		s.Conns,
		modeLabel(s.Mode),
	)
}

// modeLabel 把内部模式标识转成人话。
func modeLabel(mode string) string {
	switch mode {
	case "parallel":
		return "分片并行"
	case "single":
		return "单流降级"
	default:
		return "未知"
	}
}

// HumanBytes 把字节数格式化为易读的单位。
//
// 使用 1024 进制（KiB/MiB/GiB），与绝大多数下载工具的口径一致，
// 避免"系统显示 1.07 GB 而工具显示 1.00 GB"的困惑。
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 6; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// formatDuration 把时长格式化成紧凑形式。
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < 10*time.Second:
		// 本地/内网下载可能几十毫秒就结束，只显示整数秒会变成无意义的 "0s"。
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
