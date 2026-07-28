// Package agent implements the syncgo delta receiver.
// It is a self-contained sub-module with minimal dependencies (only go-rsync),
// designed to be embedded into the main syncgo binary via go:embed for
// cross-compilation and deployment to remote servers.
//
// package agent 实现 syncgo delta 接收端。
// 它是一个自包含的子模块，仅依赖 go-rsync，
// 通过 go:embed 嵌入主程序用于交叉编译和部署到远端服务器。
package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/henryborner/go-rsync"
)

// ReceiveOptions configures the receiver behavior.
// ReceiveOptions 配置接收端行为。
type ReceiveOptions struct {
	FilePath string // path to the existing old file / 旧文件路径
	Algo     string // checksum algorithm (md5, sha256, xxh64, xxh3) / 校验和算法
	NoCache  bool   // skip signature cache / 跳过签名缓存
}

// RunReceive executes the delta receive protocol:
// 1. Open old file → generate/load cached signature
// 2. Send signature to stdout
// 3. Read instructions from stdin → rebuild file via temp
// 4. Atomic rename temp → original
//
// RunReceive 执行 delta 接收协议：
// 1. 打开旧文件 → 生成/加载缓存签名
// 2. 发送签名到 stdout
// 3. 从 stdin 读取指令 → 通过临时文件重建
// 4. 原子替换临时文件 → 原文件
func RunReceive(opts ReceiveOptions) error {
	// 1. Open local old file
	f, err := os.Open(opts.FilePath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	fileSize := fi.Size()

	// 2. Generate or load cached signature
	blockSize := delta.CalculateBlockSize(fileSize)
	var sig *delta.Signature
	var sigWire []byte

	var cached []byte
	if !opts.NoCache {
		cached, _ = cacheLoad(opts.FilePath, fi, blockSize, opts.Algo)
	}
	if cached != nil {
		s, err := delta.WireDecodeSignature(bytes.NewReader(cached))
		if err == nil {
			sig = s
			sigWire = cached
		}
	}
	if sig == nil {
		sig = delta.GenerateSignatureReader(f, fileSize, blockSize, opts.Algo)
		var buf bytes.Buffer
		if err := delta.WireEncodeSignature(&buf, sig); err != nil {
			return fmt.Errorf("encode signature: %w", err)
		}
		sigWire = buf.Bytes()
		if !opts.NoCache {
			cacheSave(opts.FilePath, fi, blockSize, opts.Algo, sigWire)
		}
	}

	// 3. Send signature to stdout
	if _, err := os.Stdout.Write(sigWire); err != nil {
		return fmt.Errorf("send signature: %w", err)
	}

	// 4. Stream-read instructions from stdin → write to temp file
	tmpPath := opts.FilePath + ".syncgo_tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	cleanup := func() {
		out.Close()
		os.Remove(tmpPath)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			cleanup()
		}
	}()

	// 5. Read basis file for reconstruction (prefer mmap, fallback ReadFile)
	oldData, closer, err := MmapReadOnly(opts.FilePath)
	if err != nil {
		oldData, err = os.ReadFile(opts.FilePath)
		if err != nil {
			cleanup()
			return fmt.Errorf("read basis file: %w", err)
		}
	}
	if closer != nil {
		defer closer()
	}

	blockLens := make([]int32, len(sig.BlockSums))
	for i, bs := range sig.BlockSums {
		blockLens[i] = bs.Length
	}
	recon := delta.NewReconstructor(oldData, blockSize, opts.Algo, blockLens)

	err = delta.DecodeInstructionsStreamAll(os.Stdin, func(inst delta.MatchResult) error {
		return recon.WriteInstruction(out, inst)
	})
	if err != nil {
		if isEOF(err) {
			cleanup()
			return nil // file perfectly matched, no rebuild needed
		}
		cleanup()
		return fmt.Errorf("stream reconstruct: %w", err)
	}

	// 6. Close output file, atomic rename
	if err := out.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, opts.FilePath); err != nil {
		cleanup()
		return fmt.Errorf("atomic rename: %w", err)
	}
	succeeded = true
	return nil
}

// isEOF checks whether the sender closed stdin early (file is identical).
func isEOF(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// --- Signature cache ---

func cacheDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/tmp"
	}
	return filepath.Join(home, ".syncgo_cache")
}

func cacheLoad(filePath string, fi os.FileInfo, blockSize int32, algo string) ([]byte, error) {
	cachePath := cachePathFor(filePath, fi, blockSize, algo)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, nil
	}
	return data, nil
}

func cacheSave(filePath string, fi os.FileInfo, blockSize int32, algo string, data []byte) {
	cachePath := cachePathFor(filePath, fi, blockSize, algo)
	dir := filepath.Dir(cachePath)
	os.MkdirAll(dir, 0700)
	tmp := cachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return
	}
	os.Rename(tmp, cachePath)
}

func cachePathFor(filePath string, fi os.FileInfo, blockSize int32, algo string) string {
	h := sha256.Sum256([]byte(filePath))
	key := fmt.Sprintf("%s_%d_%d_%d_%s.sig",
		hex.EncodeToString(h[:8]),
		fi.ModTime().UnixNano(),
		fi.Size(),
		blockSize,
		algo,
	)
	return filepath.Join(cacheDir(), key)
}
