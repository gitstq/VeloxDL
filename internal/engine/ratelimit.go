package engine

import (
	"context"
	"sync"
	"time"
)

// Limiter 是一个基于"全局时间线预约"的字节级限速器。
//
// 为什么不用常见的令牌桶？
// 因为并行分片场景下，每个 worker 各自持桶会导致限速被绕过：
// 8 个分片同时发现有令牌，就同时放行，实际速率变成单桶的 8 倍，限速形同虚设。
//
// 这里的做法是维护一条全局的"配额时间线" next：
// 每次要发送 n 字节，就在时间线上预约一段时长 n/rate，并把自己的发送时刻
// 排到时间线末尾。于是无论多少个分片并行，总吞吐都被严格压在 rate 之下。
//
// 另一个细节：如果时间线已经落后于当前时刻（说明网络比限速还慢），
// 就直接从"现在"重新开始预约，不追补欠账。
// 这保证了限速只做"削峰"，不会把慢链路再拖慢一截。
type Limiter struct {
	rate float64 // 每秒允许通过的字节数，<=0 表示不限速

	mu   sync.Mutex
	next time.Time // 全局配额时间线的末尾
}

// NewLimiter 创建限速器。bytesPerSecond <= 0 时表示不限速。
func NewLimiter(bytesPerSecond int64) *Limiter {
	return &Limiter{rate: float64(bytesPerSecond)}
}

// Unlimited 判断是否不限速（热路径上避免加锁）。
func (l *Limiter) Unlimited() bool { return l == nil || l.rate <= 0 }

// Wait 阻塞直到允许传输 n 字节。
//
// 返回值：nil 表示可以继续发送；非 nil 表示上下文已取消。
func (l *Limiter) Wait(ctx context.Context, n int) error {
	if l.Unlimited() || n <= 0 {
		return nil
	}

	l.mu.Lock()
	now := time.Now()
	// 时间线落后（网络更慢）时不追补，直接对齐到当前时刻。
	if l.next.IsZero() || l.next.Before(now) {
		l.next = now
	}
	start := l.next
	// 本批字节占用的时长。
	l.next = l.next.Add(time.Duration(float64(n) / l.rate * float64(time.Second)))
	l.mu.Unlock()

	delay := start.Sub(now)
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Throughput 是一个滚动窗口吞吐采样器，用于动态并发调参和进度展示。
//
// 引擎需要知道"再加一条连接还能不能提速"，这个判断必须基于"最近的速率"
// 而不是"全程平均速率" —— 后者会被开头的慢启动污染，导致调参迟钝。
type Throughput struct {
	mu       sync.Mutex
	total    int64     // 累计字节数
	window   int64     // 当前窗口内字节数
	last     time.Time // 上次采样时间
	rate     float64   // 最近窗口速率（字节/秒）
	overall  float64   // 全程平均速率
	start    time.Time
	duration time.Duration
}

// NewThroughput 创建采样器。
func NewThroughput() *Throughput {
	now := time.Now()
	return &Throughput{last: now, start: now}
}

// Add 记录新到达的字节数并按需滚动窗口。
func (t *Throughput) Add(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.total += int64(n)
	t.window += int64(n)

	now := time.Now()
	if d := now.Sub(t.last); d >= sampleWindow {
		t.rate = float64(t.window) / d.Seconds()
		t.window = 0
		t.last = now
	}
	t.overall = float64(t.total) / now.Sub(t.start).Seconds()
}

// Rate 返回最近一个采样窗口的瞬时速率（字节/秒）。
func (t *Throughput) Rate() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	// 若距离上次采样已经超过两个窗口，说明速度已经掉下来了，保守地衰减估计。
	if r := t.rate; r > 0 {
		if d := time.Since(t.last); d > sampleWindow*2 {
			return r * float64(sampleWindow) / float64(d)
		}
		return r
	}
	return t.overall
}

// Overall 返回全程平均速率（字节/秒）。
func (t *Throughput) Overall() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.overall
}

// Total 返回累计传输字节数。
func (t *Throughput) Total() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.total
}

// Elapsed 返回从创建到现在经过的时间。
func (t *Throughput) Elapsed() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return time.Since(t.start)
}
