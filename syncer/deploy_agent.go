// deploy_agent.go — Deploy syncgo agent to remote Linux server (library API).
// Strategy: local pre-built binary → GitHub Release download → cross-compile.
//
// deploy_agent.go — 部署 syncgo agent 到远端 Linux 服务器（库 API）。
// 策略：本地预构建二进制 → GitHub Release 下载 → 交叉编译。
package syncer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/winezer0/syncgo/config"
	"github.com/winezer0/syncgo/transport"
)

// DeployAgentOptions configures agent deployment.
// DeployAgentOptions 配置 agent 部署参数。
type DeployAgentOptions struct {
	// Version is the syncgo release version to download.
	// Defaults to config.DefaultVersion if empty.
	// Version 要下载的 syncgo 发布版本号，为空时使用 config.DefaultVersion。
	Version string

	// BinaryPath is an optional path to a pre-built agent binary.
	// If set, skips local search and GitHub download, using this file directly.
	// BinaryPath 可选的预构建 agent 二进制路径，设置后直接使用，跳过本地查找和下载。
	BinaryPath string

	// ProjectRoot is the syncgo source tree root (containing go.mod).
	// Required only for the cross-compile fallback (Level 3).
	// If empty, cross-compile will attempt to locate go.mod automatically.
	// ProjectRoot syncgo 源码根目录（含 go.mod），仅交叉编译回退（Level 3）需要。
	// 为空时自动查找。
	ProjectRoot string

	// Progress callback for deployment status updates.
	// Progress 部署状态更新回调。
	Progress func(msg string)
}

// DeployAgent deploys the syncgo agent binary to the remote server.
// It connects, detects remote architecture, resolves the binary (3-level fallback),
// uploads it to ~/.local/bin/syncgo, sets permissions, and verifies execution.
//
// DeployAgent 将 syncgo agent 二进制部署到远端服务器。
// 连接 → 检测远端架构 → 三级回退获取二进制 → 上传到 ~/.local/bin/syncgo → 设置权限 → 验证。
func (s *Syncer) DeployAgent(ctx context.Context, opts DeployAgentOptions) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("deploy cancelled: %w", err)
	}

	// Ensure connection
	if !s.connected {
		if err := s.ConnectContext(ctx); err != nil {
			return err
		}
	}

	log := opts.Progress
	if log == nil {
		log = func(string) {}
	}

	// 1. Detect remote architecture
	log("Detecting remote architecture...")
	remoteArch, err := detectRemoteArch(s.tr)
	if err != nil {
		return fmt.Errorf("detect remote arch: %w", err)
	}
	goArch := unameToGoArch(remoteArch)
	if goArch == "" {
		return fmt.Errorf("unsupported architecture: %s", remoteArch)
	}
	log(fmt.Sprintf("Remote arch: %s (go: %s)", remoteArch, goArch))

	// 2. Resolve agent binary
	log("Resolving agent binary...")
	agentPath, cleanup, err := resolveAgentBinary(goArch, opts)
	if err != nil {
		return fmt.Errorf("resolve agent binary: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	binInfo, err := os.Stat(agentPath)
	if err != nil {
		return fmt.Errorf("stat agent binary: %w", err)
	}
	log(fmt.Sprintf("Agent ready: %s (%.1f MB)", filepath.Base(agentPath), float64(binInfo.Size())/1024/1024))

	// 3. Upload to remote
	remoteDir := ".local/bin"
	remotePath := remoteDir + "/syncgo"
	log(fmt.Sprintf("Uploading to %s:%s ...", s.server.Host, remotePath))

	if err := s.tr.MkdirAll(remoteDir); err != nil {
		return fmt.Errorf("mkdir %s: %w", remoteDir, err)
	}

	f, err := os.Open(agentPath)
	if err != nil {
		return fmt.Errorf("open binary: %w", err)
	}
	defer f.Close()

	if err := s.tr.PutFile(remotePath, f, binInfo.Size()); err != nil {
		return fmt.Errorf("upload binary: %w", err)
	}
	log("Upload complete.")

	// 4. chmod +x
	log("Setting executable permission...")
	if err := remoteChmod(s.tr, remotePath, "0755"); err != nil {
		log(fmt.Sprintf("Warning: chmod failed: %v", err))
	}

	// 5. Ensure ~/.local/bin is in PATH
	ensurePath(s.tr, remoteDir)

	// 6. Verify execution
	log("Verifying agent...")
	version, err := verifyAgent(s.tr)
	if err != nil {
		// Diagnose missing shared libraries
		if libs := checkMissingLibs(s.tr, remotePath); libs != "" {
			return fmt.Errorf("agent deployed but missing shared libraries:\n%s\nSolution: rebuild with CGO_ENABLED=0 for a fully static binary", libs)
		}
		return fmt.Errorf("agent deployed but cannot execute: %w\nTry manually: export PATH=\"$HOME/.local/bin:$PATH\" && syncgo version", err)
	}
	log(fmt.Sprintf("Agent verified: %s", version))
	return nil
}

// DeployAgentStandalone deploys the syncgo agent without an existing Syncer connection.
// Creates a temporary connection, deploys, and tears down.
//
// DeployAgentStandalone 无需已有 Syncer 连接即可部署 agent。
// 创建临时连接 → 部署 → 断开。
func DeployAgentStandalone(ctx context.Context, server config.Server, opts DeployAgentOptions) error {
	tr := transport.NewSFTP(transport.SFTPConfig{
		Host:    server.Host,
		Port:    server.Port,
		User:    server.User,
		KeyFile: server.KeyFile,
		Pass:    server.Pass,
	})

	s := &Syncer{
		server: server,
		tr:     tr,
	}

	if err := s.ConnectContext(ctx); err != nil {
		return fmt.Errorf("connect to %s@%s:%d: %w", server.User, server.Host, server.Port, err)
	}
	defer s.Close()

	return s.DeployAgent(ctx, opts)
}

// --- Internal helpers (migrated from cmd/syncgo/deploy_agent.go) ---

// detectRemoteArch runs uname -m on the remote to get CPU architecture.
func detectRemoteArch(tr *transport.SFTPTransport) (string, error) {
	out, err := tr.ExecOutput("uname -m")
	if err != nil {
		return "", fmt.Errorf("exec uname: %w", err)
	}
	return out, nil
}

// unameToGoArch maps uname -m output to Go GOARCH values.
func unameToGoArch(uname string) string {
	switch uname {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	case "armv7l", "armv6l":
		return "arm"
	case "i386", "i686":
		return "386"
	case "riscv64":
		return "riscv64"
	default:
		return ""
	}
}

// resolveAgentBinary resolves the agent binary using three-level fallback:
// 1. Explicit BinaryPath from options
// 2. Local pre-built file (syncgo_linux_<arch> in exe/CWD directory)
// 3. Download from GitHub Releases
// 4. Cross-compile from source (requires Go toolchain + project root)
func resolveAgentBinary(goArch string, opts DeployAgentOptions) (string, func(), error) {
	// Level 0: explicit binary path
	if opts.BinaryPath != "" {
		if _, err := os.Stat(opts.BinaryPath); err == nil {
			return opts.BinaryPath, nil, nil
		}
		return "", nil, fmt.Errorf("specified binary not found: %s", opts.BinaryPath)
	}

	// Level 1: local pre-built binary
	if local := findLocalAgent(goArch); local != "" {
		return local, nil, nil
	}

	// Level 2: download from GitHub Releases
	version := opts.Version
	if version == "" {
		version = config.DefaultVersion
	}
	downloaded, err := downloadFromRelease(goArch, version)
	if err == nil {
		return downloaded, func() { os.Remove(downloaded) }, nil
	}

	// Level 3: cross-compile
	projectRoot := opts.ProjectRoot
	if projectRoot == "" {
		var findErr error
		projectRoot, findErr = findProjectRoot()
		if findErr != nil {
			return "", nil, fmt.Errorf("all methods failed; download error: %w; cross-compile: cannot find project root: %v", err, findErr)
		}
	}
	tmpBinary, crossErr := crossCompile(goArch, projectRoot)
	if crossErr != nil {
		return "", nil, fmt.Errorf("all methods failed; download error: %w; cross-compile error: %v", err, crossErr)
	}
	return tmpBinary, func() { os.Remove(tmpBinary) }, nil
}

// findLocalAgent looks for a pre-built agent binary in the executable's directory or CWD.
func findLocalAgent(goArch string) string {
	name := "syncgo_linux_" + goArch

	// Try executable directory first
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() && fi.Size() > 0 {
			return candidate
		}
	}

	// Try current working directory
	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, name)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() && fi.Size() > 0 {
			return candidate
		}
	}

	return ""
}

// downloadFromRelease downloads the agent binary from GitHub Releases.
func downloadFromRelease(goArch, version string) (string, error) {
	fileName := "syncgo_linux_" + goArch
	url := fmt.Sprintf("https://github.com/winezer0/syncgo/releases/download/v%s/%s", version, fileName)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	tmpFile, err := os.CreateTemp("", "syncgo_agent_*")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()

	n, err := io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("download interrupted: %w", err)
	}
	if n < 1024 {
		os.Remove(tmpPath)
		return "", fmt.Errorf("downloaded file too small (%d bytes), likely an error page", n)
	}

	return tmpPath, nil
}

// crossCompile builds the syncgo binary for linux/<goArch>.
func crossCompile(goArch, projectRoot string) (string, error) {
	tmpFile, err := os.CreateTemp("", "syncgo_linux_*")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	buildCmd := exec.Command("go", "build", "-ldflags", "-s -w", "-o", tmpPath, "./cmd/syncgo")
	buildCmd.Dir = projectRoot
	buildCmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH="+goArch,
		"CGO_ENABLED=0",
	)

	output, err := buildCmd.CombinedOutput()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("go build: %s\n%w", string(output), err)
	}

	return tmpPath, nil
}

// findProjectRoot locates the project root by searching for go.mod.
func findProjectRoot() (string, error) {
	// Try executable directory first
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
	}
	// Try current working directory and walk up
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd, nil
}

// remoteChmod sets file permissions on the remote.
func remoteChmod(tr *transport.SFTPTransport, path, mode string) error {
	_, err := tr.ExecOutput(fmt.Sprintf("chmod %s '%s'", mode, path))
	return err
}

// ensurePath adds ~/.local/bin to PATH in .bashrc if not already present.
func ensurePath(tr *transport.SFTPTransport, dir string) {
	checkCmd := fmt.Sprintf("grep -q '%s' ~/.bashrc 2>/dev/null || echo 'export PATH=\"$HOME/%s:$PATH\"' >> ~/.bashrc", dir, dir)
	tr.ExecOutput(checkCmd)
}

// verifyAgent runs 'syncgo version' on the remote to confirm the binary can execute.
func verifyAgent(tr *transport.SFTPTransport) (string, error) {
	cmd := "$HOME/.local/bin/syncgo version"
	output, err := tr.ExecOutput(cmd)
	if err != nil {
		return "", fmt.Errorf("remote exec: %w", err)
	}
	if output == "" {
		return "", fmt.Errorf("no output from remote syncgo")
	}
	lines := strings.SplitN(output, "\n", 2)
	return lines[0], nil
}

// checkMissingLibs runs ldd on the remote binary to detect missing shared libraries.
func checkMissingLibs(tr *transport.SFTPTransport, remotePath string) string {
	cmd := fmt.Sprintf("ldd '%s' 2>&1 | grep -i 'not found' || true", remotePath)
	out, err := tr.ExecOutput(cmd)
	if err != nil || out == "" {
		return ""
	}
	var sb strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			sb.WriteString("    " + strings.TrimSpace(line) + "\n")
		}
	}
	return sb.String()
}

// getLocalArch returns the current machine's Go arch.
func getLocalArch() string {
	return runtime.GOARCH
}
