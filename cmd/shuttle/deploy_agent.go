// deploy_agent.go — Deploy shuttle agent to remote Linux server.
// Strategy: local pre-built binary → GitHub Release download → cross-compile.
// deploy_agent.go — 部署 shuttle agent 到远端 Linux 服务器。
// 策略：本地预构建二进制 → GitHub Release 下载 → 交叉编译。
package main

import (
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

	"github.com/spf13/cobra"
)

func init() {
	deployCmd := &cobra.Command{
		Use:   "deploy-agent <server name>",
		Short: "Deploy shuttle agent to a remote Linux server",
		Long: `Deploy the shuttle agent binary to the remote server via SFTP.

The agent enables rsync-style delta transfers: only changed portions
of files are sent over the network instead of full file content.

Agent binary resolution order:
  1. Local file: shuttle_linux_<arch> in the program directory
  2. Download from GitHub Releases (v` + versionStr + `)
  3. Cross-compile from source (requires local Go toolchain)

Steps performed:
  1. Connect to the server and detect CPU architecture (uname -m)
  2. Resolve agent binary (local → release → build)
  3. Upload the binary to ~/.local/bin/shuttle on the server
  4. Set executable permissions (chmod +x)
  5. Verify by running 'shuttle version' on the remote

After deployment, 'shuttle push' will automatically use delta
transfers for updated files, significantly reducing bandwidth usage.`,
		Args: cobra.ExactArgs(1),
		Run:  runDeployAgent,
	}
	deployCmd.Flags().StringVarP(&cfgPath, "config", "c", "syncd.yaml", "path to YAML config file")
	rootCmd.AddCommand(deployCmd)
}

func runDeployAgent(cmd *cobra.Command, args []string) {
	serverName := args[0]

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	server := cfg.GetServer(serverName)
	if server == nil {
		fmt.Fprintf(os.Stderr, "Server not found: %s\n", serverName)
		os.Exit(1)
	}

	// 1. Connect
	fmt.Printf("Connecting to %s@%s:%d ...\n", server.User, server.Host, server.Port)
	tr := transport.NewSFTP(transport.SFTPConfig{
		Host:    server.Host,
		Port:    server.Port,
		User:    server.User,
		KeyFile: server.KeyFile,
		Pass:    server.Pass,
	})
	if err := tr.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "Connect failed: %v\n", err)
		os.Exit(1)
	}
	defer tr.Close()
	fmt.Println("  Connected.")

	// 2. Detect remote architecture
	fmt.Print("  Detecting remote architecture... ")
	remoteArch, err := detectRemoteArch(tr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s\n", remoteArch)

	goArch := unameToGoArch(remoteArch)
	if goArch == "" {
		fmt.Fprintf(os.Stderr, "Unsupported architecture: %s\n", remoteArch)
		os.Exit(1)
	}

	// 3. Resolve agent binary: local → release → cross-compile
	agentPath, cleanup, err := resolveAgentBinary(goArch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve agent binary: %v\n", err)
		os.Exit(1)
	}
	if cleanup != nil {
		defer cleanup()
	}

	binInfo, _ := os.Stat(agentPath)
	fmt.Printf("  Agent ready: %s (%.1f MB)\n", filepath.Base(agentPath), float64(binInfo.Size())/1024/1024)

	// 4. Upload to remote
	remoteDir := ".local/bin"
	remotePath := remoteDir + "/shuttle"
	fmt.Printf("  Uploading to %s:%s ...\n", server.Host, remotePath)

	tr.MkdirAll(remoteDir)

	f, err := os.Open(agentPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Open binary failed: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	if err := tr.PutFile(remotePath, f, binInfo.Size()); err != nil {
		fmt.Fprintf(os.Stderr, "Upload failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Upload complete.")

	// 5. chmod +x
	fmt.Print("  Setting executable permission... ")
	if err := remoteChmod(tr, remotePath, "0755"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	} else {
		fmt.Println("OK")
	}

	// 6. Ensure ~/.local/bin is in PATH (add to .bashrc if missing)
	ensurePath(tr, remoteDir)

	// 7. Verify execution (with shared library diagnostics)
	// 7. 验证可执行性（含动态库依赖诊断）
	fmt.Print("  Verifying agent... ")
	version, err := verifyAgent(tr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed: %v\n", err)
		// Diagnose: check for missing shared libraries
		// 诊断：检查缺失的共享库
		if libs := checkMissingLibs(tr, remotePath); libs != "" {
			fmt.Printf("\n  ⚠ Missing shared libraries on remote:\n%s\n", libs)
			fmt.Println("  The agent binary requires dynamic libraries not available on this system.")
			fmt.Println("  Solution: rebuild with CGO_ENABLED=0 for a fully static binary.")
		} else {
			fmt.Println("\n  Agent deployed but cannot execute.")
			fmt.Println("  Possible causes: wrong architecture, missing permissions, or PATH issue.")
			fmt.Printf("  Try manually: export PATH=\"$HOME/.local/bin:$PATH\" && shuttle version\n")
		}
		return
	}
	fmt.Printf("OK — %s\n", version)
	fmt.Println("\n  Delta incremental transfers are now enabled for this server!")
}

// resolveAgentBinary resolves the agent binary using three-level fallback:
// 1. Local pre-built file (shuttle_linux_<arch> in exe directory)
// 2. Download from GitHub Releases
// 3. Cross-compile from source
// Returns: (path, cleanupFunc, error). cleanupFunc may be nil for local files.
// resolveAgentBinary 三级回退获取 agent 二进制：本地文件 → Release 下载 → 交叉编译。
func resolveAgentBinary(goArch string) (string, func(), error) {
	// Level 1: local pre-built binary
	if local := findLocalAgent(goArch); local != "" {
		fmt.Printf("  [1/3] Found local agent: %s\n", local)
		return local, nil, nil
	}
	fmt.Printf("  [1/3] Local agent not found (shuttle_linux_%s), trying release...\n", goArch)

	// Level 2: download from GitHub Releases
	downloaded, err := downloadFromRelease(goArch)
	if err == nil {
		fmt.Printf("  [2/3] Downloaded from GitHub Releases\n")
		return downloaded, func() { os.Remove(downloaded) }, nil
	}
	fmt.Printf("  [2/3] Release download failed: %v, trying cross-compile...\n", err)

	// Level 3: cross-compile
	tmpBinary, err := crossCompile(goArch)
	if err != nil {
		return "", nil, fmt.Errorf("all methods failed, last error: %w", err)
	}
	fmt.Printf("  [3/3] Cross-compiled from source\n")
	return tmpBinary, func() { os.Remove(tmpBinary) }, nil
}

// findLocalAgent looks for a pre-built agent binary in the executable's directory.
// Naming convention: shuttle_linux_<arch> (e.g. shuttle_linux_amd64, shuttle_linux_arm64).
// findLocalAgent 在可执行文件目录下查找预构建的 agent 二进制。
func findLocalAgent(goArch string) string {
	name := "shuttle_linux_" + goArch

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
// URL pattern: https://github.com/winezer0/syncgo/releases/download/v<version>/shuttle_linux_<arch>
// downloadFromRelease 从 GitHub Releases 下载 agent 二进制。
func downloadFromRelease(goArch string) (string, error) {
	fileName := "shuttle_linux_" + goArch
	url := fmt.Sprintf("https://github.com/winezer0/syncgo/releases/download/v%s/%s", versionStr, fileName)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "shuttle_agent_*")
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

// crossCompile builds the shuttle binary for linux/<goArch>.
// Returns the path to the temporary binary file.
func crossCompile(goArch string) (string, error) {
	// Find the project root (where go.mod is)
	projectRoot, err := findProjectRoot()
	if err != nil {
		return "", err
	}

	// Create temp output file
	tmpFile, err := os.CreateTemp("", "shuttle_linux_*")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	// Cross-compile: GOOS=linux GOARCH=<arch> go build -o <tmp> ./cmd/shuttle
	buildCmd := exec.Command("go", "build", "-ldflags", "-s -w", "-o", tmpPath, "./cmd/shuttle")
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
	// Try current working directory
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
	// Fallback: use cwd and hope for the best
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

// verifyAgent runs 'shuttle version' on the remote to confirm the binary can execute.
// verifyAgent 在远端执行 'shuttle version' 确认二进制可正常运行。
func verifyAgent(tr *transport.SFTPTransport) (string, error) {
	cmd := "$HOME/.local/bin/shuttle version"
	output, err := tr.ExecOutput(cmd)
	if err != nil {
		return "", fmt.Errorf("remote exec: %w", err)
	}
	if output == "" {
		return "", fmt.Errorf("no output from remote shuttle")
	}
	// Return first line of version output
	lines := strings.SplitN(output, "\n", 2)
	return lines[0], nil
}

// checkMissingLibs runs ldd on the remote binary to detect missing shared libraries.
// Returns formatted missing library info, or empty string if no issues / ldd unavailable.
// checkMissingLibs 在远端对二进制执行 ldd 检测缺失的共享库。
// 返回缺失库信息，无问题或 ldd 不可用时返回空字符串。
func checkMissingLibs(tr *transport.SFTPTransport, remotePath string) string {
	// ldd reports "not found" for missing libs; static binaries report "not a dynamic executable"
	cmd := fmt.Sprintf("ldd '%s' 2>&1 | grep -i 'not found' || true", remotePath)
	out, err := tr.ExecOutput(cmd)
	if err != nil || out == "" {
		return ""
	}
	// Format: indent each missing lib line
	var sb strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			sb.WriteString("    " + strings.TrimSpace(line) + "\n")
		}
	}
	return sb.String()
}

// getLocalArch returns the current machine's Go arch for display.
func getLocalArch() string {
	return runtime.GOARCH
}
