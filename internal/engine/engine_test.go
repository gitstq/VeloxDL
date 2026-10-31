package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// testPayload 生成 size 字节的确定性测试数据。
// 用确定性数据而不是随机数，失败时能稳定复现。
func testPayload(size int) []byte {
	buf := make([]byte, size)
	for i := range buf {
		// 一个简单的线性同余序列，保证字节分布足够"乱"，
		// 一旦分片错位就能被 sha256 发现。
		buf[i] = byte((i*31 + i/7*17) % 251)
	}
	return buf
}

// rangeServer 返回一个完整支持 Range 的测试服务器。
func rangeServer(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size := int64(len(data))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"veloxdl-test"`)

		rh := r.Header.Get("Range")
		if rh == "" {
			w.Header().Set("Content-Length", fmt.Sprint(size))
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}
			_, _ = w.Write(data)
			return
		}

		var start, end int64
		if _, err := fmt.Sscanf(strings.TrimPrefix(rh, "bytes="), "%d-%d", &start, &end); err != nil {
			// 形如 "bytes=100-" 的开放区间
			if _, err2 := fmt.Sscanf(strings.TrimPrefix(rh, "bytes="), "%d-", &start); err2 != nil {
				http.Error(w, "bad range", http.StatusBadRequest)
				return
			}
			end = size - 1
		}
		if start < 0 || start >= size || end >= size || end < start {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.Header().Set("Content-Length", fmt.Sprint(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func tempOut(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// ---- 分片规划 ----

func TestPlanChunksCoversWholeFile(t *testing.T) {
	sizes := []int64{1, 1023, 1024, 1 << 20, 5 << 20, 123456789}
	for _, size := range sizes {
		for _, conns := range []int{1, 4, 16, 32} {
			chunks := planChunks(size, DefaultMinChunkSize, conns)
			if len(chunks) == 0 {
				t.Fatalf("size=%d conns=%d: 未产生分片", size, conns)
			}
			var cursor int64
			for i, c := range chunks {
				if c.Index != i {
					t.Fatalf("size=%d: 分片下标不连续，期望 %d 实际 %d", size, i, c.Index)
				}
				if c.Start != cursor {
					t.Fatalf("size=%d: 分片 %d 起点错位，期望 %d 实际 %d", size, i, cursor, c.Start)
				}
				if c.End < c.Start || c.End >= size {
					t.Fatalf("size=%d: 分片 %d 区间越界 [%d,%d]", size, i, c.Start, c.End)
				}
				cursor = c.End + 1
			}
			if cursor != size {
				t.Fatalf("size=%d conns=%d: 分片未覆盖完整文件，覆盖到 %d", size, conns, cursor)
			}
		}
	}
}

func TestPlanChunksRespectsMinChunkSize(t *testing.T) {
	const size = 100 << 20 // 100 MiB
	chunks := planChunks(size, 4<<20, 16)
	if len(chunks) == 0 {
		t.Fatal("未产生分片")
	}
	for _, c := range chunks[:len(chunks)-1] {
		if c.Total() < 4<<20 {
			t.Fatalf("分片 %s 小于最小分片尺寸: %d", c, c.Total())
		}
	}
}

func TestPlanChunksInvalidInput(t *testing.T) {
	if got := planChunks(0, 0, 0); got != nil {
		t.Fatalf("大小为 0 时应返回 nil，实际 %v", got)
	}
	if got := planChunks(-1, 0, 0); got != nil {
		t.Fatalf("大小为负时应返回 nil，实际 %v", got)
	}
}

// ---- 端到端下载 ----

func TestDownloadParallelMatchesSource(t *testing.T) {
	data := testPayload(7<<20 + 12345) // 故意不是整数倍，考验边界处理
	srv := rangeServer(t, data)
	out := tempOut(t, "payload.bin")

	snap, err := New(Config{URLs: []string{srv.URL + "/payload.bin"}, Output: out, MaxConns: 8}).Run(context.Background())
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	if sha256Hex(got) != sha256Hex(data) {
		t.Fatal("产物内容与源文件不一致")
	}
	if snap.Saved != int64(len(data)) {
		t.Fatalf("统计字节数不符: 期望 %d 实际 %d", len(data), snap.Saved)
	}
	if snap.Mode != "parallel" {
		t.Fatalf("应走并行模式，实际 %s", snap.Mode)
	}
}

func TestDownloadMultiSource(t *testing.T) {
	data := testPayload(3 << 20)
	srv := rangeServer(t, data)
	out := tempOut(t, "multi.bin")

	// 同一个服务器给两个"镜像"地址，验证多源调度不会拼接错位。
	snap, err := New(Config{
		URLs:     []string{srv.URL + "/a.bin", srv.URL + "/b.bin"},
		Output:   out,
		MaxConns: 8,
	}).Run(context.Background())
	if err != nil {
		t.Fatalf("多源下载失败: %v", err)
	}
	got, _ := os.ReadFile(out)
	if sha256Hex(got) != sha256Hex(data) {
		t.Fatal("多源产物内容与源文件不一致")
	}
	if snap.Sources != 2 {
		t.Fatalf("可用源数量应为 2，实际 %d", snap.Sources)
	}
}

func TestDownloadResumeAfterCancel(t *testing.T) {
	data := testPayload(24 << 20)
	srv := rangeServer(t, data)
	out := tempOut(t, "resume.bin")

	// 第一次：用极低的限速 + 短超时，确保只下一小部分就被取消。
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	_, err := New(Config{
		URLs:      []string{srv.URL + "/resume.bin"},
		Output:    out,
		MaxConns:  4,
		RateLimit: 512 << 10, // 512 KiB/s
		Timeout:   30 * time.Second,
	}).Run(ctx)
	if err == nil {
		t.Fatal("预期第一次下载被中断，但成功返回")
	}

	// 中断后必须留下状态文件，否则续传无从谈起。
	if _, statErr := os.Stat(out + StateSuffix); statErr != nil {
		t.Fatalf("中断后未找到状态文件: %v", statErr)
	}
	partial, statErr := os.Stat(out)
	if statErr != nil {
		t.Fatalf("中断后未找到部分文件: %v", statErr)
	}
	if partial.Size() != int64(len(data)) {
		t.Fatalf("部分文件大小应已预分配为 %d，实际 %d", len(data), partial.Size())
	}

	// 第二次：续传完成。
	snap, err := New(Config{
		URLs:     []string{srv.URL + "/resume.bin"},
		Output:   out,
		MaxConns: 8,
		Resume:   true,
	}).Run(context.Background())
	if err != nil {
		t.Fatalf("续传失败: %v", err)
	}
	got, _ := os.ReadFile(out)
	if sha256Hex(got) != sha256Hex(data) {
		t.Fatal("续传后产物内容与源文件不一致")
	}
	if snap.Saved < int64(len(data))/2 {
		t.Fatalf("续传统计异常，已保存 %d", snap.Saved)
	}
}

func TestDownloadSingleStreamFallback(t *testing.T) {
	data := testPayload(1 << 20)
	// 一个明确不支持 Range 的服务器：忽略 Range 头，整份返回 200。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "none")
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	out := tempOut(t, "single.bin")
	snap, err := New(Config{URLs: []string{srv.URL + "/single.bin"}, Output: out, MaxConns: 8}).Run(context.Background())
	if err != nil {
		t.Fatalf("单流降级下载失败: %v", err)
	}
	got, _ := os.ReadFile(out)
	if sha256Hex(got) != sha256Hex(data) {
		t.Fatal("单流降级产物内容与源文件不一致")
	}
	if snap.Mode != "single" {
		t.Fatalf("应走单流降级模式，实际 %s", snap.Mode)
	}
}

func TestDownloadChecksumVerified(t *testing.T) {
	data := testPayload(2 << 20)
	srv := rangeServer(t, data)
	out := tempOut(t, "checked.bin")

	if _, err := New(Config{
		URLs:     []string{srv.URL + "/checked.bin"},
		Output:   out,
		MaxConns: 8,
		Checksum: "sha256:" + sha256Hex(data),
	}).Run(context.Background()); err != nil {
		t.Fatalf("校验和正确时不应报错: %v", err)
	}
}

func TestDownloadChecksumMismatch(t *testing.T) {
	data := testPayload(2 << 20)
	srv := rangeServer(t, data)
	out := tempOut(t, "bad.bin")

	_, err := New(Config{
		URLs:     []string{srv.URL + "/bad.bin"},
		Output:   out,
		MaxConns: 8,
		Checksum: "sha256:" + strings.Repeat("0", 64),
	}).Run(context.Background())
	if err == nil {
		t.Fatal("校验和不匹配时应返回错误")
	}
}

func TestDownloadRateLimit(t *testing.T) {
	const size = 4 << 20
	data := testPayload(size)
	srv := rangeServer(t, data)
	out := tempOut(t, "limited.bin")

	const limit = 2 << 20 // 2 MiB/s
	start := time.Now()
	if _, err := New(Config{
		URLs:      []string{srv.URL + "/limited.bin"},
		Output:    out,
		MaxConns:  4,
		RateLimit: limit,
	}).Run(context.Background()); err != nil {
		t.Fatalf("限速下载失败: %v", err)
	}
	elapsed := time.Since(start)

	// 4 MiB 按 2 MiB/s 至少需要约 2 秒；给一点余量避免 CI 抖动误判。
	if elapsed < 1500*time.Millisecond {
		t.Fatalf("限速未生效：传输 %d 字节仅用时 %v", size, elapsed)
	}
}

func TestDownloadNoSource(t *testing.T) {
	_, err := New(Config{URLs: nil, Output: tempOut(t, "x.bin")}).Run(context.Background())
	if err != ErrNoSource {
		t.Fatalf("无源时应返回 ErrNoSource，实际 %v", err)
	}
}

func TestDownloadBadURL(t *testing.T) {
	// 端口 1 上不可能有服务，用来验证探测失败能被正确聚合。
	_, err := New(Config{URLs: []string{"http://127.0.0.1:1/f.bin"}, Output: tempOut(t, "y.bin")}).Run(context.Background())
	if err == nil {
		t.Fatal("不可达地址应返回错误")
	}
	if !strings.Contains(err.Error(), "veloxdl") {
		t.Fatalf("错误信息应带 veloxdl 前缀，实际: %v", err)
	}
}

// TestDownloadSmallFile 覆盖"小于最小分片尺寸"的分支：
// 这类文件不该被切碎，直接整块下载即可。
func TestDownloadSmallFile(t *testing.T) {
	data := testPayload(4096)
	srv := rangeServer(t, data)
	out := tempOut(t, "small.bin")

	if _, err := New(Config{URLs: []string{srv.URL + "/small.bin"}, Output: out, MaxConns: 16}).Run(context.Background()); err != nil {
		t.Fatalf("小文件下载失败: %v", err)
	}
	got, _ := os.ReadFile(out)
	if sha256Hex(got) != sha256Hex(data) {
		t.Fatal("小文件产物不一致")
	}
}

// TestDownloadConcurrentSafe 用 -race 跑多个并发任务，
// 用来暴露分片写入与状态落盘之间的数据竞争。
func TestDownloadConcurrentSafe(t *testing.T) {
	data := testPayload(2 << 20)
	srv := rangeServer(t, data)

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out := tempOut(t, fmt.Sprintf("conc-%d.bin", i))
			if _, err := New(Config{URLs: []string{srv.URL + "/c.bin"}, Output: out, MaxConns: 8}).Run(context.Background()); err != nil {
				t.Errorf("并发任务 %d 失败: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}
