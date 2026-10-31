package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// stateVersion 是状态文件的格式版本号。
// 一旦磁盘上的版本号与当前程序不一致，一律当作过期状态丢弃（走全量重下），
// 而不是尝试兼容 —— 断点续传最怕的就是"用错误的偏移续写"，那会产出损坏文件。
const stateVersion = 1

// State 是断点续传所需的全部信息，以 JSON 落盘在目标文件旁边。
type State struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	URL       string  `json:"url"`        // 主源地址
	Output    string  `json:"output"`     // 输出文件绝对路径
	Size      int64   `json:"size"`       // 资源总大小
	ChunkSize int64   `json:"chunk_size"` // 分片大小，用于校验状态是否仍匹配
	ETag      string  `json:"etag"`       // 资源指纹，防止续传到一个已被替换的文件
	Chunks    []Chunk `json:"chunks"`     // 每个分片的进度
}

// statePath 返回输出文件对应的状态文件路径。
func statePath(output string) string {
	return output + StateSuffix
}

// StateSuffix 是状态文件后缀，暴露给 CLI 层用于展示与清理。
const StateSuffix = ".veloxdl.json"

// saveState 原子地写入状态文件。
//
// 采用"写临时文件 + rename"的两步式：rename 在同一文件系统上是原子的，
// 因此即使进程在写入过程中被 kill，也不会留下半截的、无法解析的状态文件。
func saveState(path string, st *State) error {
	st.UpdatedAt = time.Now()
	if st.CreatedAt.IsZero() {
		st.CreatedAt = st.UpdatedAt
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".veloxdl-state-*")
	if err != nil {
		return fmt.Errorf("创建临时状态文件失败: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("写入状态失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("刷盘失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("关闭状态文件失败: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("设置状态文件权限失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("替换状态文件失败: %w", err)
	}
	return nil
}

// loadState 读取状态文件；文件不存在时返回 (nil, nil)，由调用方决定是新建还是报错。
func loadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取状态文件失败: %w", err)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("%w: 无法解析 JSON", ErrStateCorrupt)
	}
	return &st, nil
}

// removeState 删除状态文件。文件已不存在时视为成功。
func removeState(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除状态文件失败: %w", err)
	}
	return nil
}

// statOutput 返回输出文件当前大小；文件不存在时返回 0。
func statOutput(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("读取输出文件信息失败: %w", err)
	}
	return fi.Size(), nil
}

// reconcile 校验"磁盘上的状态"与"远程资源 + 磁盘上的文件"是否自洽。
//
// 只有三者都吻合，续传才是安全的。任一条件不满足就返回 nil 表示"放弃续传"。
// 这是数据完整性上最不能妥协的一步：宁可重下，也不能拼错。
func reconcile(st *State, info *ResourceInfo, chunkSize int64) []*Chunk {
	if st == nil || info == nil {
		return nil
	}
	if st.Version != stateVersion {
		return nil
	}
	if st.URL != info.URL && st.URL != info.FinalURL {
		return nil // 目标换了
	}
	if st.Size != info.Size || len(st.Chunks) == 0 {
		return nil // 资源大小变了
	}
	if st.ChunkSize != chunkSize {
		return nil // 分片规划变了，无法对齐
	}
	// 远端给过 ETag 的情况下必须一致，否则资源可能已被替换。
	if info.ETag != "" && st.ETag != "" && info.ETag != st.ETag {
		return nil
	}

	onDisk, err := statOutput(st.Output)
	if err != nil {
		return nil
	}
	if onDisk != info.Size {
		return nil // 磁盘文件大小与预期不符，无法按偏移续写
	}

	// 恢复为指针切片，并做一次区间合法性检查。
	chunks := make([]*Chunk, 0, len(st.Chunks))
	sort.Slice(st.Chunks, func(i, j int) bool { return st.Chunks[i].Index < st.Chunks[j].Index })
	for i := range st.Chunks {
		c := st.Chunks[i]
		if c.Start < 0 || c.End < c.Start || c.End >= info.Size {
			return nil
		}
		if c.Downloaded < 0 || c.Downloaded > c.Total() {
			return nil
		}
		cp := c
		chunks = append(chunks, &cp)
	}

	// 校验分片是否严丝合缝地覆盖整个文件。
	var cursor int64
	for i, c := range chunks {
		if c.Start != cursor {
			return nil
		}
		if c.Index != i {
			return nil
		}
		cursor = c.End + 1
	}
	if cursor != info.Size {
		return nil
	}

	return chunks
}

// snapshot 把内存中的分片进度拷贝成可序列化的切片。
func snapshot(chunks []*Chunk) []Chunk {
	out := make([]Chunk, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, *c)
	}
	return out
}
