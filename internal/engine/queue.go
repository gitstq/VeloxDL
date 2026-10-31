package engine

import (
	"context"
	"sync"
)

// task 是队列里的一条工作单元。
//
// tries 记录已重试次数（用于指数退避与最终放弃），
// srcIdx 记录上一次使用的源，失败后会轮换到下一个镜像。
type task struct {
	chunk  *Chunk
	tries  int
	srcIdx int
}

// queue 是一个线程安全的阻塞任务队列。
//
// 为什么不用 buffered channel？
// 因为分片失败后需要"放回队列"，而 channel 的 push 可能阻塞或丢序；
// 同时我们需要知道"还有多少任务在飞行中"才能判断何时整体结束。
// 用条件变量自己实现，语义最清晰：outstanding = 已领取但未完成的任务数。
type queue struct {
	mu          sync.Mutex
	cond        *sync.Cond
	items       []*task
	closed      bool
	outstanding int
}

// newQueue 用给定分片初始化队列。
func newQueue(chunks []*Chunk) *queue {
	q := &queue{items: make([]*task, 0, len(chunks))}
	q.cond = sync.NewCond(&q.mu)
	// 按"剩余量从大到小"排序入队，让大块先被领取，
	// 这样收尾时剩下的都是小块，能显著缩短尾部的空窗期。
	sorted := make([]*Chunk, len(chunks))
	copy(sorted, chunks)
	sortChunksByRemaining(sorted)
	for _, c := range sorted {
		q.items = append(q.items, &task{chunk: c})
		q.outstanding++
	}
	return q
}

// pop 取出一条任务。队列已空且不再有新任务时返回 (nil, false)。
func (q *queue) pop() (*task, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) == 0 {
		return nil, false
	}
	t := q.items[0]
	q.items = q.items[1:]
	return t, true
}

// push 放回一条任务（例如失败重试）。
func (q *queue) push(t *task) {
	q.mu.Lock()
	q.items = append(q.items, t)
	q.mu.Unlock()
	q.cond.Broadcast()
}

// done 标记一条任务已完成（成功或彻底放弃）。
func (q *queue) done() {
	q.mu.Lock()
	q.outstanding--
	if q.outstanding <= 0 {
		q.closed = true
	}
	q.mu.Unlock()
	q.cond.Broadcast()
}

// wait 阻塞直到所有任务完成或上下文取消。
func (q *queue) wait(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		for !q.closed {
			q.cond.Wait()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// sortChunksByRemaining 按剩余字节数降序排序（插入排序，分片数量通常不大）。
func sortChunksByRemaining(chunks []*Chunk) {
	for i := 1; i < len(chunks); i++ {
		for j := i; j > 0 && chunks[j].Remaining() > chunks[j-1].Remaining(); j-- {
			chunks[j], chunks[j-1] = chunks[j-1], chunks[j]
		}
	}
}
