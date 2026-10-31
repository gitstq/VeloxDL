// Command server 是一个用于本地自测的静态文件服务器。
//
// 它刻意把"支持 Range"做成可开关的，用来验证 VeloxDL 在两种服务端下的行为：
//
//	./test/server/server -dir . -port 8099              # 正常支持 Range
//	./test/server/server -dir . -port 8099 -no-range    # 模拟不支持分片的老旧服务器
//	./test/server/server -dir . -port 8099 -delay 20ms  # 模拟高延迟链路
//
// 也可以直接用 go run：
//
//	go run ./test/server -dir /tmp/files -port 8099
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	var (
		dir     = flag.String("dir", ".", "要对外提供下载的根目录")
		port    = flag.Int("port", 8099, "监听端口")
		noRange = flag.Bool("no-range", false, "模拟不支持 Range 请求的服务端")
		delay   = flag.Duration("delay", 0, "每个请求的固定延迟，用于模拟慢链路")
	)
	flag.Parse()

	abs, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatalf("无法解析目录 %q: %v", *dir, err)
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		log.Fatalf("%q 不是一个有效目录", abs)
	}

	handler := &fileHandler{
		root:    http.Dir(abs),
		noRange: *noRange,
		delay:   *delay,
	}

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("📡 本地测试服务器启动: http://127.0.0.1%s  (根目录: %s)", addr, abs)
	if *noRange {
		log.Printf("⚠️  已开启 -no-range 模式：将忽略 Range 请求头，整份返回")
	}
	if *delay > 0 {
		log.Printf("🐢 每个请求附加延迟: %v", *delay)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// fileHandler 提供目录下载，并支持开关 Range 能力。
type fileHandler struct {
	root    http.Dir
	noRange bool
	delay   time.Duration
}

// ServeHTTP 实现 http.Handler。
func (h *fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.delay > 0 {
		time.Sleep(h.delay)
	}

	// 只放行安全的读取方法。
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	upath := strings.TrimPrefix(r.URL.Path, "/")
	if upath == "" {
		http.Error(w, "请指定文件名，例如 /sample.bin", http.StatusNotFound)
		return
	}
	// 阻断路径穿越。
	clean := filepath.Clean("/" + upath)
	if strings.Contains(clean, "..") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	f, err := h.root.Open(strings.TrimPrefix(clean, "/"))
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "not a regular file", http.StatusNotFound)
		return
	}

	size := info.Size()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", fmt.Sprintf(`"%x-%x"`, size, info.ModTime().Unix()))
	w.Header().Set("Last-Modified", info.ModTime().UTC().Format(time.RFC1123))

	if h.noRange {
		w.Header().Set("Accept-Ranges", "none")
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
		return
	}

	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
