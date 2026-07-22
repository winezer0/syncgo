package transport

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RemoteCaps describes the compression capabilities detected on the remote server.
// RemoteCaps 描述远端服务器检测到的压缩能力。
type RemoteCaps struct {
	HasTar  bool // supports tar command / 支持 tar 命令
	HasGzip bool // supports gzip command / 支持 gzip 命令
}

// DetectRemoteCaps probes the remote server for available compression commands.
// DetectRemoteCaps 探测远端服务器可用的压缩命令。
func (e *SyncEngine) DetectRemoteCaps() RemoteCaps {
	caps := RemoteCaps{}
	if e.transport == nil {
		return caps
	}
	// Check tar + gzip in one command
	out, err := e.transport.ExecOutput("command -v tar 2>/dev/null && command -v gzip 2>/dev/null")
	if err == nil {
		lines := strings.Split(out, "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if strings.Contains(l, "tar") {
				caps.HasTar = true
			}
			if strings.Contains(l, "gzip") {
				caps.HasGzip = true
			}
		}
	}
	return caps
}

// SyncFullReplace performs a full replacement sync:
// 1. Pack local source into tar.gz
// 2. Upload archive to remote temp path
// 3. Remote: clean target dir + extract archive
// 4. Cleanup temp files on both sides
//
// SyncFullReplace 执行压缩全量替换同步：
// 1. 本地目录 tar.gz 打包
// 2. SFTP 上传压缩包至远端临时路径
// 3. 远端清理目标目录 + 解压覆盖
// 4. 清理双端临时文件
func (e *SyncEngine) SyncFullReplace(opts SyncOptions) (*SyncStats, error) {
	stats := &SyncStats{}

	// Detect remote capabilities
	caps := e.DetectRemoteCaps()
	if !caps.HasTar {
		return nil, fmt.Errorf("remote server does not support 'tar' command — required for full_replace mode")
	}

	e.hook.OnSyncStart(filepath.Base(opts.Source)+" [full_replace]", 0)

	// 1. Pack local source into a temp tar.gz file
	tmpArchive, fileCount, totalSize, err := PackLocalTarGz(opts.Source, opts.Exclude, opts.SkipDots, opts.Flat)
	if err != nil {
		return nil, fmt.Errorf("pack local tar.gz: %w", err)
	}
	defer os.Remove(tmpArchive) // cleanup local temp

	stats.TotalFiles = fileCount
	stats.TotalBytes = totalSize

	if opts.DryRun {
		e.hook.OnSyncDone(stats)
		return stats, nil
	}

	// 2. Upload archive to remote temp path
	remoteTmp := fmt.Sprintf("/tmp/.shuttle_sync_%d.tar.gz", time.Now().UnixNano())
	e.hook.OnFileStart(filepath.Base(tmpArchive), totalSize)

	archiveFile, err := os.Open(tmpArchive)
	if err != nil {
		return nil, fmt.Errorf("open temp archive: %w", err)
	}
	archiveInfo, _ := archiveFile.Stat()
	archiveSize := archiveInfo.Size()

	pr := &progressReader{r: archiveFile, hook: e.hook, path: tmpArchive, size: archiveSize}
	if err := e.transport.PutFile(remoteTmp, pr, archiveSize); err != nil {
		archiveFile.Close()
		return nil, fmt.Errorf("upload archive: %w", err)
	}
	archiveFile.Close()

	stats.SentBytes = archiveSize
	e.hook.OnFileDone(FileEvent{
		RelPath:    filepath.Base(tmpArchive),
		RemotePath: remoteTmp,
		FileSize:   archiveSize,
		BytesSent:  archiveSize,
		IsNew:      true,
	})

	// 3. Remote: ensure target dir exists, clean it, then extract
	target := opts.Target
	// Build remote command: mkdir -p target && rm -rf target/* && tar -xzf tmp -C target
	var cmd string
	if caps.HasGzip {
		cmd = fmt.Sprintf("mkdir -p '%s' && find '%s' -mindepth 1 -delete 2>/dev/null; tar -xzf '%s' -C '%s'",
			EscapeShell(target), EscapeShell(target), EscapeShell(remoteTmp), EscapeShell(target))
	} else {
		// tar with built-in gzip decompression (-z flag works without separate gzip binary)
		cmd = fmt.Sprintf("mkdir -p '%s' && find '%s' -mindepth 1 -delete 2>/dev/null; tar -xzf '%s' -C '%s'",
			EscapeShell(target), EscapeShell(target), EscapeShell(remoteTmp), EscapeShell(target))
	}

	stdin, stdout, stderr, err := e.transport.Exec(cmd)
	if err != nil {
		// Try to cleanup remote temp
		e.transport.Remove(remoteTmp)
		return nil, fmt.Errorf("remote extract exec: %w", err)
	}
	stdin.Close()
	io.Copy(io.Discard, stdout)
	var errBuf strings.Builder
	io.Copy(&errBuf, stderr)
	stdout.Close()
	stderr.Close()

	// 4. Cleanup remote temp file
	e.transport.Remove(remoteTmp)

	if errBuf.Len() > 0 {
		// Non-fatal warnings from find (e.g. permission denied on some files)
		// Only treat as error if tar itself failed
		errStr := errBuf.String()
		if strings.Contains(errStr, "tar:") || strings.Contains(errStr, "Error") {
			return nil, fmt.Errorf("remote extract failed: %s", errStr)
		}
	}

	stats.NewFiles = fileCount
	e.hook.OnSyncDone(stats)
	return stats, nil
}

// PackLocalTarGz creates a tar.gz archive of the local source directory.
// Returns the path to the temp archive, file count, and total uncompressed size.
// PackLocalTarGz 将本地源目录打包为 tar.gz 临时文件。
// 返回临时文件路径、文件数量和未压缩总大小。
func PackLocalTarGz(source string, excludes []string, skipDots, flat bool) (string, int, int64, error) {
	tmpFile, err := os.CreateTemp("", "shuttle_sync_*.tar.gz")
	if err != nil {
		return "", 0, 0, err
	}
	tmpPath := tmpFile.Name()

	gw := gzip.NewWriter(tmpFile)
	tw := tar.NewWriter(gw)

	var fileCount int
	var totalSize int64

	// Determine the base prefix for tar entries
	srcInfo, err := os.Stat(source)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", 0, 0, fmt.Errorf("stat source: %w", err)
	}

	// Single file source
	if !srcInfo.IsDir() {
		if err := addFileToTar(tw, source, filepath.Base(source)); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return "", 0, 0, err
		}
		fileCount = 1
		totalSize = srcInfo.Size()
	} else {
		// Directory source: walk and add files
		basePrefix := ""
		if !flat {
			basePrefix = filepath.Base(source)
		}

		err = filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relPath, _ := filepath.Rel(source, path)
			if relPath == "." {
				return nil
			}

			// Apply exclude filters
			for _, p := range excludes {
				pat := strings.TrimRight(p, "/")
				if ok, _ := filepath.Match(pat, filepath.Base(path)); ok {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if ok, _ := filepath.Match(pat, relPath); ok {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}

			// Skip dot files
			if skipDots && strings.HasPrefix(filepath.Base(path), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			// Build tar entry name
			entryName := filepath.ToSlash(relPath)
			if basePrefix != "" {
				entryName = filepath.ToSlash(filepath.Join(basePrefix, relPath))
			}

			if d.IsDir() {
				// Add directory entry
				hdr := &tar.Header{
					Name:     entryName + "/",
					Typeflag: tar.TypeDir,
					Mode:     0755,
				}
				return tw.WriteHeader(hdr)
			}

			// Regular file
			info, err := d.Info()
			if err != nil {
				return err
			}
			if err := addFileToTar(tw, path, entryName); err != nil {
				return err
			}
			fileCount++
			totalSize += info.Size()
			return nil
		})
		if err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return "", 0, 0, err
		}
	}

	if err := tw.Close(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", 0, 0, err
	}
	if err := gw.Close(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", 0, 0, err
	}
	tmpFile.Close()

	return tmpPath, fileCount, totalSize, nil
}

// addFileToTar adds a single file to the tar writer with the given entry name.
func addFileToTar(tw *tar.Writer, filePath, entryName string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = entryName
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(tw, f)
	return err
}

// EscapeShell escapes a string for safe use in single-quoted shell context.
func EscapeShell(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}
