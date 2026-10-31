package engine

import "fmt"

// Chunk 描述文件上的一段待下载区间 [Start, End]（闭区间，单位为字节）。
//
// Downloaded 记录本分片"已经写入磁盘的字节数"，
// 因此续传时下一次请求的起点是 Start+Downloaded，而不是 Start。
type Chunk struct {
	Index      int   // 分片序号，从 0 开始
	Start      int64 // 分片在文件中的起始偏移（含）
	End        int64 // 分片在文件中的结束偏移（含）
	Downloaded int64 // 已写入的字节数
}

// RemainingStart 返回本分片尚未下载的起始偏移。
func (c *Chunk) RemainingStart() int64 { return c.Start + c.Downloaded }

// Remaining 返回本分片还剩多少字节没下载。
func (c *Chunk) Remaining() int64 { return c.End - c.RemainingStart() + 1 }

// Total 返回分片总字节数。
func (c *Chunk) Total() int64 { return c.End - c.Start + 1 }

// Done 判断分片是否已完成。
func (c *Chunk) Done() bool { return c.Downloaded >= c.Total() }

// String 返回便于日志展示的区间描述。
func (c Chunk) String() string { return fmt.Sprintf("#%d[%d-%d]", c.Index, c.Start, c.End) }

// planChunks 把一个大小为 size 的文件切分成若干固定大小的分片。
//
// 设计要点：分片数量刻意远大于最大连接数（默认 4 倍）。
// 这样做是为了避免"拖尾" —— 如果分片数恰好等于连接数，
// 一个慢连接就会让整个下载卡在最后 1/N 上；
// 分片足够碎时，快的连接会自动多领任务，整体时间趋于均衡。
func planChunks(size int64, minChunkSize int64, maxConns int) []*Chunk {
	if size <= 0 {
		return nil
	}
	if minChunkSize <= 0 {
		minChunkSize = DefaultMinChunkSize
	}
	if maxConns <= 0 {
		maxConns = DefaultMaxConns
	}

	// 目标分片数：连接数的 4 倍，同时保证每片不小于 minChunkSize，
	// 避免小文件被切得过碎（请求开销反而超过传输时间）。
	targetCount := int64(maxConns) * 4
	chunkSize := size / targetCount
	if chunkSize < minChunkSize {
		chunkSize = minChunkSize
	}
	// 分片大小对齐到 1 KiB，让 Range 边界更友好（对部分缓存/CDN 有意义）。
	const align = 1024
	if chunkSize > align {
		chunkSize = (chunkSize / align) * align
	}

	chunks := make([]*Chunk, 0, size/chunkSize+1)
	var idx int
	for off := int64(0); off < size; off += chunkSize {
		end := off + chunkSize - 1
		if end >= size {
			end = size - 1
		}
		chunks = append(chunks, &Chunk{Index: idx, Start: off, End: end})
		idx++
	}
	return chunks
}

// totalDownloaded 汇总所有分片已下载的字节数。
func totalDownloaded(chunks []*Chunk) int64 {
	var n int64
	for _, c := range chunks {
		n += c.Downloaded
	}
	return n
}

// pendingChunks 返回仍未完成的分片数量，用于进度展示与调度判断。
func pendingChunks(chunks []*Chunk) int {
	var n int
	for _, c := range chunks {
		if !c.Done() {
			n++
		}
	}
	return n
}
