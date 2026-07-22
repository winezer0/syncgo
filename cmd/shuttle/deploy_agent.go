// deploy_agent.go — Cross-compile and deploy shuttle agent to remote Linux server.
// deploy_agent.go — 交叉编译并部署 shuttle agent 到远端 Linux 服务器。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/winezer0/syncgo/config"
	"github.com/winezer0/syncgo/transport"

	"github.com/spf13/cobra"
)

func init() {
	deployCmd := &cobra.Command{
		Use:   "deploy-agent <server name>",
		Short: "Cross-compile and deploy shuttle agent to a remote Linux server",
		Long: `Automatically build the shuttle binary for the remote server's
Linux architecture (amd64/arm64) and deploy it via SFTP.

The agent enables rsync-style delta transfers: only changed portions
of files are sent over the network instead of full file content.

Steps performed:
  1. Connect to the server and detect CPU architecture (uname -m)
  2. Cross-compile shuttle for linux/<arch> using Go
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
		Host:     server.Host,
		Port:     server.Port,
		User:     server.User,
		AuthType: string(server.AuthType),
		KeyFile:  server.KeyFile,
		Pass:     server.Pass,
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

	// Map uname -m output to Go GOARCH
	goArch := unameToGoArch(remoteArch)
	if goArch == "" {
		fmt.Fprintf(os.Stderr, "Unsupported architecture: %s\n", remoteArch)
		os.Exit(1)
	}

	// 3. Cross-compile
	fmt.Printf("  Cross-compiling shuttle for linux/%s...\n", goArch)
	tmpBinary, err := crossCompile(goArch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Build failed: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(tmpBinary)

	binInfo, _ := os.Stat(tmpBinary)
	fmt.Printf("  Build OK: %s (%.1f MB)\n", filepath.Base(tmpBinary), float64(binInfo.Size())/1024/1024)

	// 4. Upload to remote
	remoteDir := ".local/bin"
	remotePath := remoteDir + "/shuttle"
	fmt.Printf("  Uploading to %s:%s ...\n", server.Host, remotePath)

	// Ensure directory exists
	tr.MkdirAll(remoteDir)

	f, err := os.Open(tmpBinary)
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

	// 7. Verify
	fmt.Print("  Verifying agent... ")
	version, err := verifyAgent(tr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verification failed: %v\n", err)
		fmt.Println("\n  Agent deployed but verification failed.")
		fmt.Printf("  You may need to add ~/.local/bin to PATH manually:\n")
		fmt.Printf("    export PATH=\"$HOME/.local/bin:$PATH\"\n")
		return
	}
	fmt.Printf("OK — %s\n", version)
	fmt.Println("\n  Delta incremental transfers are now enabled for this server!")
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

// verifyAgent runs 'shuttle version' on the remote to confirm deployment.
func verifyAgent(tr *transport.SFTPTransport) (string, error) {
	cmd := "$HOME/.local/bin/shuttle version"
	output, err := tr.ExecOutput(cmd)
	if err != nil {
		return "", fmt.Errorf("remote shuttle: %w", err)
	}
	if output == "" {
		return "", fmt.Errorf("no output from remote shuttle")
	}
	// Return first line of version output
	lines := strings.SplitN(output, "\n", 2)
	return lines[0], nil
}

// getLocalArch returns the current machine's Go arch for display.
func getLocalArch() string {
	return runtime.GOARCH
}
