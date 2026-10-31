package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// ResourceInfo 描述一个远程资源的元信息。
// 它是"能不能分片""总共多大""续传是否安全"这三个决策的唯一数据来源。
type ResourceInfo struct {
	URL      string // 原始传入地址
	FinalURL string // 经过重定向后的最终地址
	Size     int64  // 资源总字节数；-1 表示未知（chunked 传输且无 Content-Length）

	AcceptRanges  string // 服务端 Accept-Ranges 响应头的原值，通常是 "bytes" 或 "none"
	SupportsRange bool   // 是否确认支持 Range 分片

	ETag         string // 用于续传时校验资源是否被换掉
	LastModified string // 兜底的变更判断依据
	ContentType  string // 响应内容类型，仅用于展示
}

// String 返回便于日志展示的单行摘要。
func (r ResourceInfo) String() string {
	var b strings.Builder
	b.WriteString(r.URL)
	if r.FinalURL != "" && r.FinalURL != r.URL {
		fmt.Fprintf(&b, " -> %s", r.FinalURL)
	}
	if r.Size >= 0 {
		fmt.Fprintf(&b, ", size=%d", r.Size)
	} else {
		b.WriteString(", size=unknown")
	}
	fmt.Fprintf(&b, ", range=%v", r.SupportsRange)
	return b.String()
}

// probeOne 探测单个源。
//
// 探测策略是"先礼后兵"：
//  1. 优先发 HEAD，代价最小；
//  2. 很多 CDN / 对象存储对 HEAD 返回残缺头，此时降级为带 Range: bytes=0-0 的 GET，
//     用 206 状态码 + Content-Range 里的总大小反推资源信息；
//  3. 仍然失败才认定该源不可用。
func probeOne(ctx context.Context, client *http.Client, rawURL string, headers map[string]string) (*ResourceInfo, error) {
	info := &ResourceInfo{URL: rawURL, Size: -1}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return nil, &SourceError{Err: err}
	}
	applyHeaders(req, headers)

	resp, err := client.Do(req)
	if err == nil {
		func() {
			defer resp.Body.Close()
			info.FinalURL = resp.Request.URL.String()
			info.AcceptRanges = resp.Header.Get("Accept-Ranges")
			info.ETag = resp.Header.Get("ETag")
			info.LastModified = resp.Header.Get("Last-Modified")
			info.ContentType = resp.Header.Get("Content-Type")
			info.Size = resp.ContentLength
		}()
	} else {
		resp = nil
	}

	// HEAD 没拿到可信大小，或者压根没成功 —— 用 Range GET 再试一次。
	if info.Size < 0 {
		rInfo, rErr := probeByRange(ctx, client, rawURL, headers)
		if rErr != nil {
			if err != nil {
				// HEAD 与 Range GET 双双失败，返回 HEAD 的错误更有代表性。
				return nil, &SourceError{URL: rawURL, Err: err}
			}
			return nil, rErr
		}
		// Range GET 成功，说明服务端确实支持分片。
		rInfo.ETag = firstNonEmpty(rInfo.ETag, info.ETag)
		rInfo.LastModified = firstNonEmpty(rInfo.LastModified, info.LastModified)
		rInfo.ContentType = firstNonEmpty(rInfo.ContentType, info.ContentType)
		return rInfo, nil
	}

	// HEAD 成功且拿到了大小：再确认一次 Range 是否真的可用。
	// 仅凭 Accept-Ranges 头并不可靠，部分服务声明了却返回 200。
	verified, vErr := verifyRangeSupport(ctx, client, info.FinalURL, headers, info.Size)
	if vErr == nil {
		info.SupportsRange = verified
	} else {
		// 验证失败不致命，退回用响应头做保守判断。
		info.SupportsRange = strings.EqualFold(strings.TrimSpace(info.AcceptRanges), "bytes")
	}

	return info, nil
}

// probeByRange 通过 "Range: bytes=0-0" 的 GET 请求反推资源信息。
// 响应 206 时，Content-Range: bytes 0-0/SIZE 里的 SIZE 就是资源总大小。
func probeByRange(ctx context.Context, client *http.Client, rawURL string, headers map[string]string) (*ResourceInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, &SourceError{URL: rawURL, Err: err}
	}
	req.Header.Set("Range", "bytes=0-0")
	applyHeaders(req, headers)

	resp, err := client.Do(req)
	if err != nil {
		return nil, &SourceError{URL: rawURL, Err: err}
	}
	defer resp.Body.Close()
	discardBody(resp)

	info := &ResourceInfo{
		URL:           rawURL,
		FinalURL:      resp.Request.URL.String(),
		Size:          -1,
		AcceptRanges:  resp.Header.Get("Accept-Ranges"),
		ETag:          resp.Header.Get("ETag"),
		LastModified:  resp.Header.Get("Last-Modified"),
		ContentType:   resp.Header.Get("Content-Type"),
		SupportsRange: false,
	}

	if resp.StatusCode == http.StatusPartialContent {
		if total := parseTotalFromContentRange(resp.Header.Get("Content-Range")); total > 0 {
			info.Size = total
			info.SupportsRange = true
			info.AcceptRanges = "bytes"
			return info, nil
		}
	}

	if resp.StatusCode == http.StatusOK {
		// 服务端忽略了 Range，整份返回。此时也能拿到大小。
		if resp.ContentLength >= 0 {
			info.Size = resp.ContentLength
			return info, nil
		}
		return nil, &SourceError{URL: rawURL, Err: ErrRangeUnsupported}
	}

	return nil, &SourceError{URL: rawURL, Err: fmt.Errorf("探测失败，HTTP %d", resp.StatusCode)}
}

// verifyRangeSupport 真正发一次 Range 请求，确认服务端是否返回 206。
// 只在资源大于 1 字节时才有意义。
func verifyRangeSupport(ctx context.Context, client *http.Client, url string, headers map[string]string, size int64) (bool, error) {
	if size <= 1 {
		return false, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", size-1, size-1))
	applyHeaders(req, headers)

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	discardBody(resp)

	if resp.StatusCode == http.StatusPartialContent {
		return true, nil
	}
	if resp.StatusCode == http.StatusOK && resp.ContentLength == size {
		// 忽略 Range，整份返回 —— 不支持分片。
		return false, nil
	}
	// 其它状态码（403/416 等）当作不支持，走单流更稳妥。
	return false, nil
}

// probeAll 并发探测所有源，返回成功的结果以及全部失败的聚合错误。
//
// 这里刻意保留"部分失败"的能力：某个镜像挂掉不应该让整个下载任务失败，
// 这正是多源设计要解决的问题。
func probeAll(ctx context.Context, client *http.Client, urls []string, headers map[string]string) ([]*ResourceInfo, []*SourceError) {
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		ok     = make([]*ResourceInfo, 0, len(urls))
		failed = make([]*SourceError, 0, len(urls))
		sem    = make(chan struct{}, 8) // 限制探测并发，避免镜像过多时打爆 fd
	)

	for i, u := range urls {
		wg.Add(1)
		go func(idx int, raw string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			info, err := probeOne(ctx, client, raw, headers)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				se, okErr := err.(*SourceError)
				if !okErr {
					se = &SourceError{Err: err}
				}
				se.Index, se.URL = idx, raw
				failed = append(failed, se)
				return
			}
			ok = append(ok, info)
		}(i, u)
	}
	wg.Wait()

	// 保持与输入顺序一致，保证"第一个成功的源"是用户最优先指定的那个。
	order := make(map[string]int, len(urls))
	for i, u := range urls {
		order[u] = i
	}
	sortByOrder(ok, order)
	sortErrors(failed)

	return ok, failed
}

// applyHeaders 把用户自定义请求头写入请求。
func applyHeaders(req *http.Request, headers map[string]string) {
	for k, v := range headers {
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseTotalFromContentRange 从 "bytes 0-0/1234" 中解析出总大小 1234。
func parseTotalFromContentRange(s string) int64 {
	s = strings.TrimSpace(s)
	idx := strings.LastIndex(s, "/")
	if idx < 0 {
		return -1
	}
	var total int64
	if _, err := fmt.Sscanf(strings.TrimSpace(s[idx+1:]), "%d", &total); err != nil {
		return -1
	}
	return total
}

// sortByOrder 按原始顺序稳定排序探测结果。
func sortByOrder(list []*ResourceInfo, order map[string]int) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && order[list[j].URL] < order[list[j-1].URL]; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// sortErrors 按源下标排序失败记录，保证输出稳定。
func sortErrors(list []*SourceError) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Index < list[j-1].Index; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// discardBody 读取并丢弃响应体，确保底层连接可以被复用（否则无法 keep-alive）。
// 限制最多读取 64 KiB，避免探测阶段把整个文件都读下来。
func discardBody(resp *http.Response) {
	const maxDrain = 64 << 10
	_, _ = io.CopyN(io.Discard, resp.Body, maxDrain)
}
