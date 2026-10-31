package engine

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gitstq/VeloxDL/internal/verify"
)

// 全局默认参数。
const (
	// DefaultMinChunkSize 是分片的最小尺寸。
	// 低于这个值时 HTTP 请求头的开销会盖过分片带来的收益。
	DefaultMinChunkSize = 1 << 20 // 1 MiB

	// DefaultMaxConns 是单文件默认的最大连接数。
	DefaultMaxConns = 16

	// DefaultRetries 是单个分片的默认最大重试次数。
	DefaultRetries = 5

	// DefaultTimeout 是单个分片请求的默认超时。
	DefaultTimeout = 60 * time.Second

	// sampleWindow 是吞吐采样窗口。
	sampleWindow = 500 * time.Millisecond

	// copyBufferSize 是写盘缓冲区大小，兼顾 syscall 次数与内存占用。
	copyBufferSize = 256 << 10 // 256 KiB

	// stateFlushInterval 是状态文件落盘间隔。
	stateFlushInterval = 2 * time.Second
)

// Config 描述一次下载任务的全部参数。
//
// URL 与 URLs 二选一：URLs 用于多镜像聚合（把不同分片打到不同源上），
// 这是"把所有可用带宽用起来"的关键手段。
type Config struct {
	URLs         []string          // 候选源，按优先级排列
	Output       string            // 输出文件路径
	MinChunkSize int64             // 分片最小尺寸，<=0 取默认值
	MaxConns     int               // 最大连接数，<=0 取默认值
	RateLimit    int64             // 限速，单位字节/秒，<=0 不限
	Retries      int               // 分片重试次数，<0 取默认值
	Timeout      time.Duration     // 分片请求超时，<=0 取默认值
	Resume       bool              // 是否尝试断点续传
	Checksum     string            // 期望校验和，形如 "sha256:abcd..."；为空则不校验
	Headers      map[string]string // 自定义请求头
	UserAgent    string            // User-Agent，为空取默认值
	Quiet        bool              // 静默模式（不打印进度）
	OnProgress   func(Snapshot)    // 进度回调，可能并发调用
}

// Snapshot 是给 UI 层消费的一次性进度快照。
type Snapshot struct {
	Output  string
	Total   int64   // 文件总大小
	Saved   int64   // 已写入字节（含续传基线）
	Rate    float64 // 最近窗口速率（字节/秒）
	Overall float64 // 全程平均速率（字节/秒）
	Conns   int     // 当前并发数
	Pending int     // 未完成分片数
	Chunks  int     // 分片总数
	Elapsed time.Duration
	Mode    string // "parallel"（分片并行）或 "single"（单流降级）
	Sources int    // 可用源数量
}

// Percent 返回下载完成百分比（0~100）。
func (s Snapshot) Percent() float64 {
	if s.Total <= 0 {
		return 0
	}
	return float64(s.Saved) / float64(s.Total) * 100
}

// ETA 返回预计剩余时间；速率为 0 时返回 0。
func (s Snapshot) ETA() time.Duration {
	if s.Rate <= 0 {
		return 0
	}
	return time.Duration(float64(s.Total-s.Saved)/s.Rate) * time.Second
}

// Engine 是下载任务的执行体。每个 Engine 只服务一次下载，不可复用。
type Engine struct {
	cfg    Config
	client *http.Client
	tp     *Throughput // 全程共享的吞吐采样器，供进度展示与最终汇总使用

	mu        sync.Mutex
	chunks    []*Chunk
	saved     int64 // 已写入总字节（含续传基线）
	pending   int
	conns     int
	mode      string
	errs      []*SourceError
	sourceOK  int
	deadSrc   map[int]bool
	srcCursor int
	srcMu     sync.Mutex
}

// New 创建引擎并准备好 HTTP 客户端。
func New(cfg Config) *Engine {
	cfg.Output = filepath.Clean(cfg.Output)
	return &Engine{
		cfg:     cfg,
		client:  newClient(cfg),
		tp:      NewThroughput(),
		deadSrc: make(map[int]bool),
	}
}

// newClient 构造为高并发下载调优过的 HTTP 客户端。
//
// 关键点：MaxIdleConnsPerHost 必须足够大，否则分片连接会频繁重建，
// 每次都要重新握手，吞吐会显著下降。
func newClient(cfg Config) *http.Client {
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   128,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
		// 不跟随重定向时我们可以自己控制；这里保留默认跟随，
		// 因为镜像地址经常 302 到 CDN。
	}
}

// Run 执行完整下载流程，返回本次任务的统计信息。
//
// 流程：探测 → 规划 → 续传对齐 → 预分配 → 并发拉取 → 校验 → 清理。
func (e *Engine) Run(ctx context.Context) (*Snapshot, error) {
	if len(e.cfg.URLs) == 0 {
		return nil, ErrNoSource
	}

	// ---- 1. 探测所有源 ----
	probeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	infos, failErrs := probeAll(probeCtx, e.client, e.cfg.URLs, e.cfg.Headers)
	cancel()

	e.errs = failErrs
	if len(infos) == 0 {
		return nil, fmt.Errorf("%w（共 %d 个源全部探测失败）", ErrNoSource, len(e.cfg.URLs))
	}

	// 以第一个成功的源为准，其余作为镜像；大小不一致的镜像直接剔除，
	// 否则跨源拼接会产出字节错位的文件。
	primary := infos[0]
	mirrors := make([]*ResourceInfo, 0, len(infos))
	for _, in := range infos {
		if in.Size != primary.Size {
			e.errs = append(e.errs, &SourceError{URL: in.URL, Err: ErrSizeMismatch})
			continue
		}
		mirrors = append(mirrors, in)
	}
	e.sourceOK = len(mirrors)

	// 未知大小（chunked 且无 Content-Length）只能单流顺序写。
	if primary.Size <= 0 {
		return e.runSingle(ctx, mirrors)
	}
	if !primary.SupportsRange {
		return e.runSingle(ctx, mirrors)
	}
	return e.runParallel(ctx, primary, mirrors)
}

// runParallel 走"分片 + 多源并行"的主路径。
func (e *Engine) runParallel(ctx context.Context, primary *ResourceInfo, mirrors []*ResourceInfo) (*Snapshot, error) {
	size := primary.Size
	chunkSize := chunkSizeFor(size, e.cfg.MinChunkSize, e.cfg.MaxConns)

	// ---- 2. 断点续传对齐 ----
	sp := statePath(e.cfg.Output)
	var st *State
	if e.cfg.Resume {
		var err error
		if st, err = loadState(sp); err != nil {
			// 状态文件损坏不致命，退化为全量重下。
			st = nil
		}
	}

	all := reconcile(st, primary, chunkSize)
	resumed := all != nil
	if !resumed {
		all = planChunks(size, chunkSize, e.cfg.MaxConns)
		// ---- 3. 预分配文件 ----
		// 预分配让所有分片都能用 pwrite 乱序落盘，
		// 同时尽早暴露"磁盘空间不足"，而不是下到 99% 才失败。
		f, err := os.OpenFile(e.cfg.Output, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("创建输出文件失败: %w", err)
		}
		if err := f.Truncate(size); err != nil {
			f.Close()
			return nil, fmt.Errorf("预分配文件失败（磁盘空间不足？）: %w", err)
		}
		if err := f.Close(); err != nil {
			return nil, fmt.Errorf("关闭输出文件失败: %w", err)
		}
	}

	// e.chunks 始终是"完整分片列表"：状态文件必须能全量覆盖文件，
	// 否则下次续传时无法校验区间完整性。队列里只放未完成的。
	e.mu.Lock()
	e.chunks = all
	e.mode = "parallel"
	e.mu.Unlock()

	pending := make([]*Chunk, 0, len(all))
	var base int64
	for _, c := range all {
		base += c.Downloaded
		if !c.Done() {
			pending = append(pending, c)
		}
	}
	e.mu.Lock()
	e.saved, e.pending = base, len(pending)
	e.mu.Unlock()

	// 全部分片都已完成（例如上一次下载只差收尾）→ 直接进入校验。
	if len(pending) == 0 {
		return e.finish(ctx, primary, size)
	}

	if err := e.pump(ctx, primary, mirrors, size, sp, chunkSize, pending); err != nil {
		// 出错时保留状态文件，方便下次 -c 续传。
		e.flushState(sp, primary, chunkSize)
		return nil, err
	}
	return e.finish(ctx, primary, size)
}

// runSingle 是不支持 Range 时的降级路径：单条连接顺序写。
//
// 这条路径同样支持限速与断点续传（通过 Range 之外的 If-Range 无法保证，
// 所以只支持"文件已完整"的快进，不支持半截续传）。
func (e *Engine) runSingle(ctx context.Context, mirrors []*ResourceInfo) (*Snapshot, error) {
	e.mu.Lock()
	e.mode = "single"
	e.conns = 1
	e.mu.Unlock()

	var lastErr error
	for _, src := range mirrors {
		snap, err := e.fetchStream(ctx, src)
		if err == nil {
			snap.Mode = "single"
			snap.Output = e.cfg.Output
			snap.Sources = len(mirrors)
			if err := e.verify(); err != nil {
				return nil, err
			}
			return snap, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = ErrNoSource
	}
	return nil, lastErr
}

// fetchStream 用单条连接把整个资源写入输出文件。
func (e *Engine) fetchStream(ctx context.Context, src *ResourceInfo) (*Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.FinalURL, nil)
	if err != nil {
		return nil, &SourceError{URL: src.URL, Err: err}
	}
	applyHeaders(req, e.cfg.Headers)
	req.Header.Set("User-Agent", userAgent(e.cfg))

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, &SourceError{URL: src.URL, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		_, _ = io.CopyN(io.Discard, resp.Body, 4096)
		return nil, &SourceError{URL: src.URL, Err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}

	f, err := os.OpenFile(e.cfg.Output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("创建输出文件失败: %w", err)
	}
	defer f.Close()

	limiter := NewLimiter(e.cfg.RateLimit)
	tp := e.tp
	buf := make([]byte, copyBufferSize)

	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nr, rerr := resp.Body.Read(buf)
		if nr > 0 {
			if err := limiter.Wait(ctx, nr); err != nil {
				return nil, err
			}
			nw, werr := f.Write(buf[:nr])
			if werr != nil {
				return nil, fmt.Errorf("写入文件失败: %w", werr)
			}
			written += int64(nw)
			tp.Add(nw)
			e.publish(written, 1, 1, 1, sizeOr(written, src.Size), "single", len(e.cfg.URLs))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, fmt.Errorf("读取响应失败: %w", rerr)
		}
	}

	if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("刷盘失败: %w", err)
	}

	return &Snapshot{
		Output:  e.cfg.Output,
		Total:   written,
		Saved:   written,
		Overall: tp.Overall(),
		Elapsed: tp.Elapsed(),
	}, nil
}

// pump 启动 worker 池、并发调参器与状态落盘协程，并等待全部完成。
func (e *Engine) pump(ctx context.Context, primary *ResourceInfo, mirrors []*ResourceInfo, size int64, sp string, chunkSize int64, pending []*Chunk) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 任务队列：用条件变量实现的阻塞队列，保证 worker 空闲时能被唤醒。
	q := newQueue(pending)

	limiter := NewLimiter(e.cfg.RateLimit)
	tp := e.tp

	// 打开输出文件（读写模式，用于 pwrite）。
	f, err := os.OpenFile(e.cfg.Output, os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开输出文件失败: %w", err)
	}
	defer f.Close()

	// 并发门：动态控制同时活跃的连接数。
	maxConns := e.cfg.MaxConns
	if maxConns <= 0 {
		maxConns = DefaultMaxConns
	}
	gate := newGate(initialConns(maxConns))

	// 把起始并发同步进快照：否则在极快的内网/本地下载里，
	// 调参器还没来得及跑第一轮，进度条会一直显示 conns 0。
	e.mu.Lock()
	e.conns = gate.currentLimit()
	e.mu.Unlock()

	retries := e.cfg.Retries
	if retries < 0 {
		retries = DefaultRetries
	}

	var wg sync.WaitGroup
	workerErr := make(chan error, maxConns+1)

	// ---- worker 池 ----
	for i := 0; i < maxConns; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				if err := gate.acquire(runCtx); err != nil {
					return
				}
				t, ok := q.pop()
				if !ok {
					gate.release()
					return
				}
				err := e.fetchChunk(runCtx, f, t.chunk, mirrors, id, limiter, tp, size)
				gate.release()
				if err != nil {
					if runCtx.Err() != nil {
						return
					}
					t.tries++
					if t.tries > retries {
						q.done()
						select {
						case workerErr <- err:
						default:
						}
						cancel() // 触发整体退出
						return
					}
					// 指数退避后重试；换一个源，避开故障镜像。
					t.srcIdx = (t.srcIdx + 1) % max(1, len(mirrors))
					backoff(runCtx, t.tries)
					q.push(t)
					continue
				}
				q.done()
			}
		}(i)
	}

	// ---- 并发调参器 ----
	stop := make(chan struct{})
	var tunerWg sync.WaitGroup
	tunerWg.Add(1)
	go func() {
		defer tunerWg.Done()
		e.tune(runCtx, gate, tp, size, stop)
	}()

	// ---- 状态落盘协程 ----
	var flushWg sync.WaitGroup
	flushWg.Add(1)
	go func() {
		defer flushWg.Done()
		ticker := time.NewTicker(stateFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				e.flushState(sp, primary, chunkSize)
			}
		}
	}()

	// 等待所有任务结束或出错。
	q.wait(runCtx)
	close(stop)
	wg.Wait()
	tunerWg.Wait()
	flushWg.Wait()

	if err := f.Sync(); err != nil {
		return fmt.Errorf("刷盘失败: %w", err)
	}

	select {
	case err := <-workerErr:
		return err
	default:
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrCanceled, err)
	}
	return nil
}

// fetchChunk 拉取单个分片并写入文件的对应偏移。
//
// 每个分片独立携带重试所需的全部状态：只要把 Downloaded 之前的部分跳过，
// 就能安全地从断点继续，而已写入的字节绝不会被重复覆盖。
func (e *Engine) fetchChunk(ctx context.Context, f *os.File, c *Chunk, mirrors []*ResourceInfo, workerID int, limiter *Limiter, tp *Throughput, size int64) error {
	src := mirrors[pickSource(mirrors, workerID, c.Index)]
	start := c.RemainingStart()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.FinalURL, nil)
	if err != nil {
		return err
	}
	applyHeaders(req, e.cfg.Headers)
	req.Header.Set("User-Agent", userAgent(e.cfg))
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, c.End))

	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 200 意味着服务端忽略了 Range：这条路径下绝不能按偏移写，
	// 否则会把整份内容写到错误的位置。
	if resp.StatusCode == http.StatusOK {
		_, _ = io.CopyN(io.Discard, resp.Body, 4096)
		return ErrRangeUnsupported
	}
	if resp.StatusCode != http.StatusPartialContent {
		_, _ = io.CopyN(io.Discard, resp.Body, 4096)
		return fmt.Errorf("分片 %s 请求失败，HTTP %d", c, resp.StatusCode)
	}

	buf := make([]byte, copyBufferSize)
	woff := start
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		nr, rerr := resp.Body.Read(buf)
		if nr > 0 {
			// 限速：先"买票"再写盘，保证长期平均速率收敛到设定值。
			if err := limiter.Wait(ctx, nr); err != nil {
				return err
			}
			if _, werr := f.WriteAt(buf[:nr], woff); werr != nil {
				return fmt.Errorf("写入分片 %s 失败: %w", c, werr)
			}
			woff += int64(nr)
			e.advance(c, int64(nr))
			tp.Add(nr)
			e.emit(size, "parallel", len(mirrors))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("读取分片 %s 失败: %w", c, rerr)
		}
	}

	if woff != c.End+1 {
		return fmt.Errorf("分片 %s 数据不完整，期望 %d 实际 %d", c, c.End+1, woff)
	}
	return nil
}

// tune 周期性地根据吞吐变化调整并发连接数。
//
// 策略类似 TCP 拥塞控制的"探测 - 回退"：
//   - 提速明显（>8%）→ 试探性加一条连接；
//   - 明显掉速（>12%）→ 退一条，说明链路已经饱和或被限速。
//
// 这样既能在空闲链路上吃满带宽，又不会在链路饱和时把连接打成一团糟。
func (e *Engine) tune(ctx context.Context, gate *gate, tp *Throughput, size int64, stop <-chan struct{}) {
	maxConns := e.cfg.MaxConns
	if maxConns <= 0 {
		maxConns = DefaultMaxConns
	}

	// 给链路一点慢启动时间，避免用最初几百毫秒的抖动做判断。
	select {
	case <-time.After(sampleWindow * 2):
	case <-stop:
		return
	case <-ctx.Done():
		return
	}

	prev := tp.Rate()
	for i := 0; ; i++ {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-time.After(sampleWindow * 2):
		}

		cur := tp.Rate()
		switch {
		case prev > 0 && cur > prev*1.08:
			gate.raise(maxConns)
		case prev > 0 && cur < prev*0.88:
			gate.lower()
		}
		prev = cur

		e.mu.Lock()
		e.conns = gate.currentLimit()
		e.mu.Unlock()
		e.emit(size, "parallel", e.sourceOK)
	}
}

// finish 做收尾工作：校验和验证 + 清理状态文件，并返回最终快照。
func (e *Engine) finish(ctx context.Context, info *ResourceInfo, size int64) (*Snapshot, error) {
	if err := e.verify(); err != nil {
		return nil, err
	}
	// 只有确认无误后才删除状态文件，保证中断后一定能续传。
	if err := removeState(statePath(e.cfg.Output)); err != nil {
		return nil, err
	}
	return e.snapshotOf(info, size), nil
}

// flushState 把当前进度写入状态文件。
func (e *Engine) flushState(path string, info *ResourceInfo, chunkSize int64) {
	e.mu.Lock()
	st := &State{
		Version:   stateVersion,
		URL:       info.URL,
		Output:    e.cfg.Output,
		Size:      info.Size,
		ChunkSize: chunkSize,
		ETag:      info.ETag,
		Chunks:    snapshot(e.chunks),
	}
	e.mu.Unlock()

	// 落盘失败不影响下载主流程，仅忽略（下一周期会重试）。
	_ = saveState(path, st)
}

// advance 记录某个分片新写入的字节数。
func (e *Engine) advance(c *Chunk, n int64) {
	e.mu.Lock()
	c.Downloaded += n
	e.saved += n
	e.mu.Unlock()
}

// emit 对外推送一次进度快照。
func (e *Engine) emit(total int64, mode string, sources int) {
	if e.cfg.OnProgress == nil {
		return
	}
	snap := e.snapshotOf(&ResourceInfo{URL: e.cfg.URLs[0], Size: total}, total)
	snap.Mode = mode
	snap.Sources = sources
	e.cfg.OnProgress(*snap)
}

// verify 在下载完成后校验文件完整性。
//
// 这一步是"数据正确性"的最后一道闸门：并行分片 + 乱序落盘一旦有 bug，
// 几乎必然体现为校验和不匹配，而不是程序报错，所以必须默认执行。
func (e *Engine) verify() error {
	if e.cfg.Checksum == "" {
		return nil
	}
	spec, err := verify.ParseSpec(e.cfg.Checksum)
	if err != nil {
		return err
	}
	return spec.File(e.cfg.Output)
}

// publish 用于单流模式推送进度。
func (e *Engine) publish(saved int64, conns, pending, chunks int, total int64, mode string, sources int) {
	if e.cfg.OnProgress == nil {
		return
	}
	e.mu.Lock()
	e.saved, e.conns, e.pending = saved, conns, pending
	e.mu.Unlock()
	e.emit(total, mode, sources)
}

// snapshotOf 组装当前进度快照。
func (e *Engine) snapshotOf(info *ResourceInfo, size int64) *Snapshot {
	e.mu.Lock()
	saved, conns, pending, chunks, mode, sources := e.saved, e.conns, e.pending, len(e.chunks), e.mode, e.sourceOK
	e.mu.Unlock()

	snap := &Snapshot{
		Output:  e.cfg.Output,
		Total:   size,
		Saved:   saved,
		Conns:   conns,
		Pending: pending,
		Chunks:  chunks,
		Mode:    mode,
		Sources: sources,
	}
	if e.tp != nil {
		snap.Rate = e.tp.Rate()
		snap.Overall = e.tp.Overall()
		snap.Elapsed = e.tp.Elapsed()
	}
	return snap
}

// chunkSizeFor 根据文件大小与配置算出实际分片大小。
func chunkSizeFor(size, minChunkSize int64, maxConns int) int64 {
	if minChunkSize <= 0 {
		minChunkSize = DefaultMinChunkSize
	}
	if maxConns <= 0 {
		maxConns = DefaultMaxConns
	}
	target := int64(maxConns) * 4
	cs := size / target
	if cs < minChunkSize {
		cs = minChunkSize
	}
	const align = 1024
	if cs > align {
		cs = (cs / align) * align
	}
	return cs
}

// initialConns 决定起始并发数：既不吃满，也不从 1 慢慢爬。
func initialConns(maxConns int) int {
	if maxConns <= 2 {
		return maxConns
	}
	if maxConns <= 8 {
		return 4
	}
	return 6
}

// pickSource 为某个分片选择源：优先按 worker 轮转，保证分片散落到不同镜像上。
func pickSource(mirrors []*ResourceInfo, workerID, chunkIndex int) int {
	if len(mirrors) <= 1 {
		return 0
	}
	return (workerID + chunkIndex) % len(mirrors)
}

// backoff 按重试次数做指数退避，带上限，避免雪崩式重试。
func backoff(ctx context.Context, tries int) {
	d := time.Duration(1<<uint(min(tries, 5))) * 100 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// sizeOr 在 size 有效时返回 size，否则返回 fallback。
func sizeOr(size, fallback int64) int64 {
	if size > 0 {
		return size
	}
	return fallback
}

// userAgent 返回实际使用的 User-Agent。
func userAgent(cfg Config) string {
	if cfg.UserAgent != "" {
		return cfg.UserAgent
	}
	return "VeloxDL/1.0 (+https://github.com/gitstq/VeloxDL)"
}
