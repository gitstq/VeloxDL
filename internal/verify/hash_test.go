package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp 写入临时文件并返回路径。
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}
	return p
}

// sha 便捷计算 sha256。
func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestParseSpecColonForm(t *testing.T) {
	spec, err := ParseSpec("sha256:" + sha("hello"))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if spec.Algorithm != "sha256" {
		t.Fatalf("算法解析错误: %s", spec.Algorithm)
	}
	if spec.Hex != sha("hello") {
		t.Fatalf("摘要解析错误: %s", spec.Hex)
	}
}

func TestParseSpecEqualsForm(t *testing.T) {
	spec, err := ParseSpec("sha256=" + sha("hello"))
	if err != nil {
		t.Fatalf("等号形式解析失败: %v", err)
	}
	if spec.Algorithm != "sha256" || spec.Hex != sha("hello") {
		t.Fatalf("解析结果不符: %+v", spec)
	}
}

func TestParseSpecBareDigest(t *testing.T) {
	// 64 位十六进制应自动推断为 sha256。
	spec, err := ParseSpec(sha("hello"))
	if err != nil {
		t.Fatalf("裸摘要解析失败: %v", err)
	}
	if spec.Algorithm != "sha256" {
		t.Fatalf("算法推断错误: %s", spec.Algorithm)
	}
}

func TestParseSpecCaseInsensitive(t *testing.T) {
	upper := strings.ToUpper(sha("hello"))
	spec, err := ParseSpec("SHA256:" + upper)
	if err != nil {
		t.Fatalf("大写摘要解析失败: %v", err)
	}
	if spec.Hex != sha("hello") {
		t.Fatalf("未统一为小写: %s", spec.Hex)
	}
}

func TestParseSpecEmpty(t *testing.T) {
	if _, err := ParseSpec(""); err != ErrNoChecksum {
		t.Fatalf("空字符串应返回 ErrNoChecksum，实际 %v", err)
	}
	if _, err := ParseSpec("   "); err != ErrNoChecksum {
		t.Fatalf("空白字符串应返回 ErrNoChecksum，实际 %v", err)
	}
}

func TestParseSpecUnsupportedAlgorithm(t *testing.T) {
	_, err := ParseSpec("crc32:1234")
	if err == nil {
		t.Fatal("不支持的算法应返回错误")
	}
	if !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("错误信息应提示不支持: %v", err)
	}
}

func TestParseSpecInvalidHex(t *testing.T) {
	if _, err := ParseSpec("sha256:zzzz"); err == nil {
		t.Fatal("非法十六进制应返回错误")
	}
}

func TestFileMatches(t *testing.T) {
	p := writeTemp(t, "veloxdl")
	spec, err := ParseSpec("sha256:" + sha("veloxdl"))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if err := spec.File(p); err != nil {
		t.Fatalf("内容一致时不应报错: %v", err)
	}
}

func TestFileMismatch(t *testing.T) {
	p := writeTemp(t, "veloxdl")
	spec, err := ParseSpec("sha256:" + sha("something-else"))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	err = spec.File(p)
	if err == nil {
		t.Fatal("内容不一致时应返回错误")
	}
	if !strings.Contains(err.Error(), "校验和不匹配") {
		t.Fatalf("错误信息应包含“校验和不匹配”: %v", err)
	}
}

func TestFileMissing(t *testing.T) {
	spec, err := ParseSpec("sha256:" + sha("x"))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if err := spec.File(filepath.Join(t.TempDir(), "nope.bin")); err == nil {
		t.Fatal("文件不存在时应返回错误")
	}
}

func TestSumReader(t *testing.T) {
	spec := &Spec{Algorithm: "md5", Hex: "900150983cd24fb0d6963f7d28e17f72"} // md5("abc")
	got, err := spec.Sum(strings.NewReader("abc"))
	if err != nil {
		t.Fatalf("计算失败: %v", err)
	}
	if got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Fatalf("md5 结果错误: %s", got)
	}
}

func TestSupportedAlgorithms(t *testing.T) {
	got := SupportedAlgorithms()
	want := []string{"md5", "sha1", "sha256", "sha512"}
	if len(got) != len(want) {
		t.Fatalf("算法列表长度不符: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("算法列表不符: 期望 %v 实际 %v", want, got)
		}
	}
}
