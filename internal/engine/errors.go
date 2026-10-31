// Package engine 实现 VeloxDL 的核心下载引擎。
//
// 引擎对外只暴露一个职责：把一个（或多个镜像上的）远程资源，
// 以尽可能高的吞吐、尽可能稳的方式搬运到本地文件。
package engine

import (
	"errors"
	"fmt"
)

// 引擎层的哨兵错误，调用方应当使用 errors.Is 判断，而不是比较字符串。
var (
	// ErrNoSource 表示所有候选源都探测失败，没有任何可用镜像。
	ErrNoSource = errors.New("veloxdl: 没有可用的下载源")

	// ErrSizeMismatch 表示多个镜像返回的 Content-Length 不一致，
	// 无法安全地把分片散落到不同源上。
	ErrSizeMismatch = errors.New("veloxdl: 镜像之间文件大小不一致")

	// ErrChecksumMismatch 表示下载完成后的校验和与期望值不符。
	ErrChecksumMismatch = errors.New("veloxdl: 校验和不匹配")

	// ErrStateCorrupt 表示断点续传状态文件损坏或与当前目标不匹配。
	ErrStateCorrupt = errors.New("veloxdl: 断点续传状态已损坏")

	// ErrCanceled 表示下载被外部取消。
	ErrCanceled = errors.New("veloxdl: 下载已取消")

	// ErrRangeUnsupported 表示服务端不支持 Range 请求，只能走单流降级。
	ErrRangeUnsupported = errors.New("veloxdl: 服务端不支持分片下载")
)

// SourceError 描述"某一个源"的失败信息。
// 引擎会尽量容忍个别源的失败，只有全部失败时才向上抛出 ErrNoSource。
type SourceError struct {
	Index int    // 源在配置中的下标
	URL   string // 源地址
	Err   error  // 底层错误
}

// Error 实现 error 接口。
func (e *SourceError) Error() string {
	return fmt.Sprintf("veloxdl: 源 #%d (%s) 失败: %v", e.Index, e.URL, e.Err)
}

// Unwrap 返回底层错误，便于 errors.Is / errors.As 穿透。
func (e *SourceError) Unwrap() error { return e.Err }
