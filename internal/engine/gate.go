package engine

import (
	"context"
	"sync"
)

// gate 是一个"容量可动态调整"的并发闸门。
//
// 它与固定容量信号量的区别在于：运行中可以 raise/lower，
// 这正是自适应并发调参所需要的 —— 我们不想杀掉再重建 worker，
// 只想让一部分 worker 暂时领不到活。
type gate struct {
	mu     sync.Mutex
	cond   *sync.Cond
	limit  int
	active int
	max    int
}

// newGate 创建闸门，初始容量为 n。
func newGate(n int) *gate {
	g := &gate{limit: n, max: n}
	g.cond = sync.NewCond(&g.mu)
	return g
}

// acquire 等待一个空位；上下文取消时返回错误。
func (g *gate) acquire(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		for g.active >= g.limit {
			g.cond.Wait()
		}
		g.active++
		done <- nil
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release 归还一个空位，并唤醒等待者。
func (g *gate) release() {
	g.mu.Lock()
	if g.active > 0 {
		g.active--
	}
	g.mu.Unlock()
	g.cond.Broadcast()
}

// raise 提高并发上限（每次 +1），不超过 max。
func (g *gate) raise(max int) {
	g.mu.Lock()
	g.max = max
	if g.limit < max {
		g.limit++
	}
	g.mu.Unlock()
	g.cond.Broadcast()
}

// lower 降低并发上限（每次 -1），不低于 1，且不缩到当前活跃数以下。
func (g *gate) lower() {
	g.mu.Lock()
	if g.limit > 1 && g.limit > g.active {
		g.limit--
	}
	g.mu.Unlock()
	g.cond.Broadcast()
}

// currentLimit 返回当前并发上限。
func (g *gate) currentLimit() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.limit
}
