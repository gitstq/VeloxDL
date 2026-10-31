// Package verify 提供下载产物的完整性校验能力。
package verify

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
)

// 支持的哈希算法及其构造函数。
var algorithms = map[string]func() hash.Hash{
	"md5":    md5.New,
	"sha1":   sha1.New,
	"sha256": sha256.New,
	"sha512": sha512.New,
}

// 校验相关的错误。
var (
	// ErrNoChecksum 表示没有提供校验和，跳过校验。
	ErrNoChecksum = errors.New("verify: 未提供校验和")

	// ErrUnsupportedAlgorithm 表示使用了不支持的哈希算法。
	ErrUnsupportedAlgorithm = errors.New("verify: 不支持的哈希算法")

	// ErrMismatch 表示计算得到的校验和与期望值不一致。
	ErrMismatch = errors.New("verify: 校验和不匹配")
)

// Spec 描述一条校验和期望值。
type Spec struct {
	Algorithm string // 算法名，如 sha256
	Hex       string // 期望的十六进制摘要
}

// ParseSpec 解析 "sha256:abcdef..." 形式的校验和。
//
// 为方便使用，也接受三种宽容写法：
//   - "sha256:ABCD" → 统一转小写后再比对；
//   - "sha256=ABCD" → 冒号与等号等价；
//   - "ABCD"（40/32/64/128 位十六进制）→ 按长度自动推断算法。
func ParseSpec(s string) (*Spec, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, ErrNoChecksum
	}

	var algo, hexStr string
	if i := strings.IndexAny(s, ":= "); i > 0 {
		algo, hexStr = s[:i], s[i+1:]
	} else {
		hexStr = s
	}
	algo = strings.ToLower(strings.TrimSpace(algo))
	hexStr = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(hexStr), "-", ""))

	if algo == "" {
		algo = inferAlgorithm(hexStr)
	}
	if algo == "" {
		return nil, fmt.Errorf("%w: 无法从 %q 推断算法，请使用 sha256:xxx 形式", ErrUnsupportedAlgorithm, s)
	}
	if _, ok := algorithms[algo]; !ok {
		return nil, fmt.Errorf("%w: %s（支持 md5/sha1/sha256/sha512）", ErrUnsupportedAlgorithm, algo)
	}
	if _, err := hex.DecodeString(hexStr); err != nil {
		return nil, fmt.Errorf("校验和格式非法: %w", err)
	}

	return &Spec{Algorithm: algo, Hex: hexStr}, nil
}

// inferAlgorithm 根据摘要长度推断算法。
func inferAlgorithm(h string) string {
	switch len(h) {
	case 32:
		return "md5"
	case 40:
		return "sha1"
	case 64:
		return "sha256"
	case 128:
		return "sha512"
	}
	return ""
}

// File 计算文件的哈希并与期望值比对。
func (s *Spec) File(path string) error {
	if s == nil {
		return ErrNoChecksum
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开待校验文件失败: %w", err)
	}
	defer f.Close()

	h, err := s.Sum(f)
	if err != nil {
		return err
	}
	if !strings.EqualFold(h, s.Hex) {
		return fmt.Errorf("%w: 期望 %s:%s，实际 %s", ErrMismatch, s.Algorithm, s.Hex, h)
	}
	return nil
}

// Sum 对任意 io.Reader 计算哈希，返回小写十六进制字符串。
func (s *Spec) Sum(r io.Reader) (string, error) {
	if s == nil {
		return "", ErrNoChecksum
	}
	ctor, ok := algorithms[s.Algorithm]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedAlgorithm, s.Algorithm)
	}
	h := ctor()
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("计算哈希失败: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// String 返回 "algo:hex" 形式的可读表示。
func (s Spec) String() string {
	return s.Algorithm + ":" + s.Hex
}

// SupportedAlgorithms 返回支持的算法列表（已排序），用于帮助信息输出。
func SupportedAlgorithms() []string {
	return []string{"md5", "sha1", "sha256", "sha512"}
}
