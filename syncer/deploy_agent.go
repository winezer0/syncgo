// deploy_agent.go — Deploy syncgo agent to remote Linux server (library API).
// Strategy: local pre-built binary → GitHub Release download → embedded cross-compile.
//
// deploy_agent.go — 部署 syncgo agent 到远端 Linux 服务器（库 API）。
// 策略：本地预构建二进制 → GitHub Release 下载 → 嵌入源码交叉编译。
package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
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

	// Progress callback for deployment status updates.
	// Progress 部署状态更新回调。
	Progress func(msg string)
}

// DeployAgent deploys the syncgo agent binary to the remote server.
// It connects, detects remote architecture, resolves the binary (4-level fallback),
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

// AgentExists checks whether the syncgo agent binary is already installed on the remote.
// It tries PATH first, then common install locations (~/.local/bin, /usr/local/bin).
//
// AgentExists 检查远端是否已安装 syncgo agent 二进制。
func (s *Syncer) AgentExists() bool {
	if !s.connected || s.tr == nil {
		return false
	}
	cmd := `command -v syncgo 2>/dev/null || test -x $HOME/.local/bin/syncgo && echo found || test -x /usr/local/bin/syncgo && echo found || true`
	out, err := s.tr.ExecOutput(cmd)
	return err == nil && strings.TrimSpace(out) != ""
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

// resolveAgentBinary resolves the agent binary using multi-level fallback:
// 1. Explicit BinaryPath from options
// 2. Local pre-built file (syncgo_linux_<arch> in exe/CWD directory)
// 3. Download from GitHub Releases
// 4. Cross-compile from embedded source (go:embed, works even as library)
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

	// Level 3: cross-compile from embedded source (works even as library)
	embedded, embedErr := crossCompileEmbedded(goArch)
	if embedErr == nil {
		return embedded, func() { os.Remove(embedded) }, nil
	}

	return "", nil, fmt.Errorf("all methods failed; download: %v; embedded compile: %v", err, embedErr)
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
// Strategy: try the specified version first, then fall back to the latest release.
// downloadFromRelease 从 GitHub Releases 下载 agent 二进制。
// 策略：优先下载相同版本，失败时回退到最新 Release。
func downloadFromRelease(goArch, version string) (string, error) {
	fileName := "syncgo_linux_" + goArch

	// Attempt 1: exact version match
	path, err := downloadReleaseAsset(fileName, version)
	if err == nil {
		return path, nil
	}

	// Attempt 2: fall back to latest release
	latest, latestErr := getLatestReleaseVersion()
	if latestErr != nil {
		return "", fmt.Errorf("version %s failed: %w; latest release lookup failed: %v", version, err, latestErr)
	}
	if latest == version {
		return "", fmt.Errorf("version %s failed: %w (already the latest release)", version, err)
	}

	path, err2 := downloadReleaseAsset(fileName, latest)
	if err2 != nil {
		return "", fmt.Errorf("version %s failed: %w; latest (v%s) also failed: %v", version, err, latest, err2)
	}
	return path, nil
}

// downloadReleaseAsset downloads a single release asset by version.
// downloadReleaseAsset 按版本号下载单个 Release 产物。
func downloadReleaseAsset(fileName, version string) (string, error) {
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

// getLatestReleaseVersion queries the GitHub API for the latest release tag.
// Returns the version string without the 'v' prefix (e.g. "0.0.3").
// getLatestReleaseVersion 通过 GitHub API 获取最新 Release 版本号（不含 'v' 前缀）。
func getLatestReleaseVersion() (string, error) {
	url := "https://api.github.com/repos/winezer0/syncgo/releases/latest"

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API HTTP %d", resp.StatusCode)
	}

	var result struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse GitHub API response: %w", err)
	}
	if result.TagName == "" {
		return "", fmt.Errorf("no tag_name in GitHub API response")
	}

	// Strip 'v' prefix: "v0.0.3" → "0.0.3"
	return strings.TrimPrefix(result.TagName, "v"), nil
}

// crossCompileEmbedded cross-compiles the agent from the embedded source tree.
// It extracts the embedded agent source to a temp directory, generates a standalone
// go.mod, and runs go build. This works even when syncgo is used as a library
// (go get), since the agent source is baked into the binary via go:embed.
//
// crossCompileEmbedded 从嵌入的源码树交叉编译 agent。
// 将嵌入的 agent 源码提取到临时目录，生成独立的 go.mod，然后执行 go build。
// 即使 syncgo 作为库使用（go get）也能工作，因为 agent 源码通过 go:embed 烘焙进了二进制。
func crossCompileEmbedded(goArch string) (string, error) {
	// Create temp directory for extracted source
	tmpDir, err := os.MkdirTemp("", "syncgo_agent_src_*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract embedded agent source to temp directory.
	// The embedded FS has an "agent/" prefix; strip it so files land at the root.
	agentFS, err := fs.Sub(agentSource, "agent")
	if err != nil {
		return "", fmt.Errorf("access embedded agent subtree: %w", err)
	}
	if err := extractEmbeddedFS(agentFS, tmpDir); err != nil {
		return "", fmt.Errorf("extract embedded source: %w", err)
	}

	// Rewrite import path in cmd/syncgo-agent/main.go:
	// The embedded source uses "github.com/winezer0/syncgo/syncer/agent" (parent module path),
	// but in the standalone temp module the agent package is at the root.
	// Replace with a short module-relative import.
	rewriteImports(tmpDir)

	// Generate standalone go.mod for the extracted source.
	// The agent source is embedded without go.mod (to stay in the parent module),
	// so we generate a minimal one here with only the go-rsync dependency.
	if err := generateAgentGoMod(tmpDir); err != nil {
		return "", fmt.Errorf("generate agent go.mod: %w", err)
	}

	// Run go mod tidy to resolve dependencies
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	tidyCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := tidyCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go mod tidy (embedded): %s\n%w", string(output), err)
	}

	// Build the agent binary from extracted source
	tmpFile, err := os.CreateTemp("", "syncgo_linux_*")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	buildCmd := exec.Command("go", "build", "-ldflags", "-s -w", "-o", tmpPath, "./cmd/syncgo-agent")
	buildCmd.Dir = tmpDir
	buildCmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH="+goArch,
		"CGO_ENABLED=0",
	)

	output, err := buildCmd.CombinedOutput()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("go build (embedded): %s\n%w", string(output), err)
	}

	return tmpPath, nil
}

// generateAgentGoMod writes a minimal go.mod for the agent sub-module.
// The agent only depends on go-rsync, keeping the dependency tree minimal.
func generateAgentGoMod(dir string) error {
	const goMod = `module syncgo-agent

go 1.25.0

require github.com/henryborner/go-rsync v0.4.0
`
	return os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644)
}

// rewriteImports fixes import paths in cmd/syncgo-agent/main.go for standalone compilation.
// In the embedded source, the import is "github.com/winezer0/syncgo/syncer/agent",
// but in the temp module the agent package is at the root as package "agent".
func rewriteImports(tmpDir string) {
	mainGo := filepath.Join(tmpDir, "cmd", "syncgo-agent", "main.go")
	data, err := os.ReadFile(mainGo)
	if err != nil {
		return
	}
	// Replace the full module import with the standalone module path
	data = []byte(strings.ReplaceAll(string(data),
		`"github.com/winezer0/syncgo/syncer/agent"`,
		`"syncgo-agent"`,
	))
	os.WriteFile(mainGo, data, 0644)
}

// extractEmbeddedFS writes an embed.FS to a destination directory on disk.
func extractEmbeddedFS(fsys fs.FS, dstDir string) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}
		return os.WriteFile(target, data, 0644)
	})
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
