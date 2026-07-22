package syncer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/winezer0/syncgo/config"
)

func TestUnameToGoArch(t *testing.T) {
	tests := []struct {
		uname string
		want  string
	}{
		{"x86_64", "amd64"},
		{"amd64", "amd64"},
		{"aarch64", "arm64"},
		{"arm64", "arm64"},
		{"armv7l", "arm"},
		{"armv6l", "arm"},
		{"i386", "386"},
		{"i686", "386"},
		{"riscv64", "riscv64"},
		{"unknown_arch", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := unameToGoArch(tt.uname)
		if got != tt.want {
			t.Errorf("unameToGoArch(%q) = %q, want %q", tt.uname, got, tt.want)
		}
	}
}

func TestFindLocalAgent_NotFound(t *testing.T) {
	// In a test environment, there should be no agent binary around.
	// This verifies the function returns "" when nothing is found.
	got := findLocalAgent("nonexistent_arch")
	if got != "" {
		t.Errorf("findLocalAgent(nonexistent_arch) = %q, want empty", got)
	}
}

func TestFindLocalAgent_Found(t *testing.T) {
	// Create a temp file mimicking a local agent binary
	tmpDir := t.TempDir()
	agentName := "syncgo_linux_amd64"
	agentPath := filepath.Join(tmpDir, agentName)
	if err := os.WriteFile(agentPath, []byte("fake binary content"), 0755); err != nil {
		t.Fatal(err)
	}

	// Change CWD to tmpDir so findLocalAgent can find it
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	got := findLocalAgent("amd64")
	if got == "" {
		t.Error("findLocalAgent(amd64) returned empty, expected a path")
	}
	if filepath.Base(got) != agentName {
		t.Errorf("findLocalAgent(amd64) = %q, want basename %q", got, agentName)
	}
}

func TestResolveAgentBinary_ExplicitPath(t *testing.T) {
	// Create a fake binary
	tmpFile, err := os.CreateTemp("", "fake_agent_*")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.WriteString("fake binary")
	tmpFile.Close()
	defer os.Remove(tmpPath)

	opts := DeployAgentOptions{BinaryPath: tmpPath}
	got, cleanup, err := resolveAgentBinary("amd64", opts)
	if err != nil {
		t.Fatalf("resolveAgentBinary with explicit path: %v", err)
	}
	if cleanup != nil {
		cleanup()
	}
	if got != tmpPath {
		t.Errorf("got %q, want %q", got, tmpPath)
	}
}

func TestResolveAgentBinary_ExplicitPathNotFound(t *testing.T) {
	opts := DeployAgentOptions{BinaryPath: "/nonexistent/path/to/binary"}
	_, _, err := resolveAgentBinary("amd64", opts)
	if err == nil {
		t.Error("expected error for nonexistent BinaryPath, got nil")
	}
}

func TestDeployAgentOptions_DefaultVersion(t *testing.T) {
	// Verify that empty Version falls back to config.DefaultVersion
	opts := DeployAgentOptions{}
	version := opts.Version
	if version == "" {
		// The resolveAgentBinary function handles the fallback internally,
		// but we verify the constant is accessible
		if config.DefaultVersion == "" {
			t.Error("config.DefaultVersion should not be empty")
		}
	}
}

func TestDeployAgentStandalone_InvalidServer(t *testing.T) {
	// Test that DeployAgentStandalone fails gracefully with an unreachable server
	server := config.Server{
		Host: "192.0.2.1", // TEST-NET, should be unreachable
		Port: 22,
		User: "test",
		Pass: "test",
	}
	opts := DeployAgentOptions{Version: "0.0.1"}

	ctx, cancel := context.WithTimeout(context.Background(), 5e9) // 5 seconds
	defer cancel()

	err := DeployAgentStandalone(ctx, server, opts)
	if err == nil {
		t.Error("expected error for unreachable server, got nil")
	}
}

func TestGetLocalArch(t *testing.T) {
	arch := getLocalArch()
	if arch == "" {
		t.Error("getLocalArch() returned empty string")
	}
	// Should be one of the known Go architectures
	known := map[string]bool{"amd64": true, "arm64": true, "386": true, "arm": true}
	if !known[arch] {
		t.Logf("getLocalArch() = %q (not in common set, but may be valid)", arch)
	}
}

func TestCrossCompileEmbedded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cross-compile test in short mode")
	}
	// Verify that the embedded agent source can be cross-compiled for linux/amd64.
	// This is the core capability that enables agent deployment even when
	// syncgo is used as a library (go get).
	binary, err := crossCompileEmbedded("amd64")
	if err != nil {
		t.Fatalf("crossCompileEmbedded(amd64): %v", err)
	}
	defer os.Remove(binary)

	// Verify the binary was actually created and is non-empty
	fi, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("stat compiled binary: %v", err)
	}
	if fi.Size() < 1000 {
		t.Errorf("binary too small: %d bytes, expected at least 1KB", fi.Size())
	}
	t.Logf("embedded cross-compile succeeded: %s (%.1f MB)", binary, float64(fi.Size())/1024/1024)
}

func TestEmbeddedAgentSource(t *testing.T) {
	// Verify the embedded FS contains the expected files
	required := []string{
		"agent/receive.go",
		"agent/mmap.go",
		"agent/mmap_unix.go",
		"agent/mmap_windows.go",
		"agent/cmd/syncgo-agent/main.go",
	}
	for _, name := range required {
		data, err := agentSource.ReadFile(name)
		if err != nil {
			t.Errorf("agentSource.ReadFile(%q): %v", name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("agentSource.ReadFile(%q): empty file", name)
		}
	}
}
