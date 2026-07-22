package syncer

import (
	"context"
	"testing"
	"time"

	"github.com/winezer0/syncgo/config"
	"github.com/winezer0/syncgo/transport"
)

// TestNewOptionsDefaults verifies that New() applies correct defaults.
func TestNewOptionsDefaults(t *testing.T) {
	s := New(Options{
		Host: "example.com",
		User: "deploy",
	})
	defer s.Close()

	if s.server.Host != "example.com" {
		t.Errorf("Host = %q, want %q", s.server.Host, "example.com")
	}
	if s.server.Port != 22 {
		t.Errorf("Port = %d, want 22", s.server.Port)
	}
	if s.server.User != "deploy" {
		t.Errorf("User = %q, want %q", s.server.User, "deploy")
	}
	if s.retry.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", s.retry.MaxRetries)
	}
	if s.retry.DelayMs != 1000 {
		t.Errorf("DelayMs = %d, want 1000", s.retry.DelayMs)
	}
	if s.cfg.Workers != 4 {
		t.Errorf("Workers = %d, want 4", s.cfg.Workers)
	}
}

// TestNewOptionsCustom verifies custom options are respected.
func TestNewOptionsCustom(t *testing.T) {
	s := New(Options{
		Host:       "10.0.0.1",
		Port:       2222,
		User:       "admin",
		AuthType:   config.AuthPassword,
		Password:   "secret",
		Protect:    []string{"*.log", "data/"},
		Workers:    8,
		MaxRetries: 5,
		DelayMs:    500,
	})
	defer s.Close()

	if s.server.Port != 2222 {
		t.Errorf("Port = %d, want 2222", s.server.Port)
	}
	if s.server.AuthType != config.AuthPassword {
		t.Errorf("AuthType = %q, want %q", s.server.AuthType, config.AuthPassword)
	}
	if s.server.Pass != "secret" {
		t.Errorf("Pass = %q, want %q", s.server.Pass, "secret")
	}
	if len(s.server.Protect) != 2 {
		t.Errorf("Protect len = %d, want 2", len(s.server.Protect))
	}
	if s.cfg.Workers != 8 {
		t.Errorf("Workers = %d, want 8", s.cfg.Workers)
	}
	if s.retry.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", s.retry.MaxRetries)
	}
	if s.retry.DelayMs != 500 {
		t.Errorf("DelayMs = %d, want 500", s.retry.DelayMs)
	}
}

// TestNewSyncerFromConfig verifies config-based constructor.
func TestNewSyncerFromConfig(t *testing.T) {
	cfg := config.Config{
		Workers: 6,
		Retry:   config.RetryConfig{MaxRetries: 2, DelayMs: 200},
		Servers: []config.Server{
			{Name: "vps", Host: "1.2.3.4", Port: 22, User: "root"},
		},
	}

	s, err := NewSyncer(cfg, "vps")
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	defer s.Close()

	if s.server.Host != "1.2.3.4" {
		t.Errorf("Host = %q, want %q", s.server.Host, "1.2.3.4")
	}
	if s.retry.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", s.retry.MaxRetries)
	}

	// Non-existent server
	_, err = NewSyncer(cfg, "nonexist")
	if err == nil {
		t.Error("expected error for non-existent server")
	}
}

// TestConnectContextCancelled verifies context cancellation during connect.
func TestConnectContextCancelled(t *testing.T) {
	s := New(Options{
		Host:       "192.0.2.1", // RFC 5737 TEST-NET, unreachable
		User:       "nobody",
		MaxRetries: 3,
		DelayMs:    100,
	})
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := s.ConnectContext(ctx)
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}
	t.Logf("got expected error: %v", err)
}

// TestConnectContextTimeout verifies context timeout during connect.
func TestConnectContextTimeout(t *testing.T) {
	s := New(Options{
		Host:       "192.0.2.1", // unreachable
		User:       "nobody",
		MaxRetries: 10,
		DelayMs:    5000,
	})
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := s.ConnectContext(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error with timeout context")
	}
	// Should return quickly due to context cancellation, not wait for all retries
	if elapsed > 2*time.Second {
		t.Errorf("took too long: %v (should be cancelled quickly)", elapsed)
	}
	t.Logf("cancelled after %v: %v", elapsed, err)
}

// TestUploadFileContextCancelled verifies upload respects context.
func TestUploadFileContextCancelled(t *testing.T) {
	s := New(Options{
		Host: "192.0.2.1",
		User: "nobody",
	})
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.UploadFileContext(ctx, "somefile.txt", "/remote/file.txt")
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}
}

// TestExecRemoteContextCancelled verifies exec respects context.
func TestExecRemoteContextCancelled(t *testing.T) {
	s := New(Options{
		Host: "192.0.2.1",
		User: "nobody",
	})
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.ExecRemoteContext(ctx, "uname -a")
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}
}

// TestDownloadDirContextCancelled verifies download respects context.
func TestDownloadDirContextCancelled(t *testing.T) {
	s := New(Options{
		Host: "192.0.2.1",
		User: "nobody",
	})
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.DownloadDirContext(ctx, "/remote/dir", "local_dir")
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}
}

// TestSyncDirOptionsStruct verifies SyncDirOptions fields compile correctly.
func TestSyncDirOptionsStruct(t *testing.T) {
	incr := true
	opts := SyncDirOptions{
		Mode:        config.SyncFullReplace,
		Delete:      true,
		Exclude:     []string{"*.tmp"},
		Checksum:    true,
		Flat:        true,
		ShowDots:    false,
		Incremental: &incr,
		DryRun:      true,
		Workers:     2,
	}

	if opts.Mode != config.SyncFullReplace {
		t.Errorf("Mode = %q, want full_replace", opts.Mode)
	}
	if *opts.Incremental != true {
		t.Error("Incremental should be true")
	}
}

// TestHookFuncAdapter verifies HookFunc implements transport.SyncHook.
func TestHookFuncAdapter(t *testing.T) {
	var started bool
	var completed bool
	var fileEvents []transport.FileEvent

	hook := transport.HookFunc{
		OnStart: func(taskName string, totalFiles int) error {
			started = true
			return nil
		},
		OnDone: func(evt transport.FileEvent) error {
			fileEvents = append(fileEvents, evt)
			return nil
		},
		OnComplete: func(stats *transport.SyncStats) error {
			completed = true
			return nil
		},
	}

	// Verify it satisfies the interface
	var _ transport.SyncHook = hook

	// Simulate hook calls
	hook.OnSyncStart("test-task", 10)
	if !started {
		t.Error("OnStart not called")
	}

	hook.OnFileDone(transport.FileEvent{RelPath: "a.txt", IsNew: true})
	if len(fileEvents) != 1 {
		t.Errorf("fileEvents len = %d, want 1", len(fileEvents))
	}

	hook.OnSyncDone(&transport.SyncStats{TotalFiles: 10})
	if !completed {
		t.Error("OnComplete not called")
	}

	// Nil fields should not panic
	emptyHook := transport.HookFunc{}
	emptyHook.OnSyncStart("x", 0)
	emptyHook.OnFileStart("y", 100)
	emptyHook.OnFileProgress("y", 50, 100)
	emptyHook.OnFileDone(transport.FileEvent{})
	emptyHook.OnSyncDone(nil)
}

// TestHookFuncProgress verifies progress callback works.
func TestHookFuncProgress(t *testing.T) {
	var progressCalls int
	var lastSent, lastTotal int64

	hook := transport.HookFunc{
		OnProgress: func(path string, sent int64, total int64) {
			progressCalls++
			lastSent = sent
			lastTotal = total
		},
	}

	hook.OnFileProgress("/file.bin", 512, 1024)
	if progressCalls != 1 || lastSent != 512 || lastTotal != 1024 {
		t.Errorf("progress: calls=%d sent=%d total=%d", progressCalls, lastSent, lastTotal)
	}
}

// TestIsConnectedWithoutConnect verifies IsConnected returns false initially.
func TestIsConnectedWithoutConnect(t *testing.T) {
	s := New(Options{Host: "example.com", User: "test"})
	defer s.Close()

	if s.IsConnected() {
		t.Error("IsConnected should be false before Connect")
	}
}

// TestExecHooksStructure verifies ExecHooks accepts config.Hooks correctly.
func TestExecHooksStructure(t *testing.T) {
	hooks := config.Hooks{
		Before:  []string{"systemctl stop app"},
		After:   []string{"systemctl start app"},
		OnError: config.HookAbort,
	}

	if !hooks.HasHooks() {
		t.Error("HasHooks should be true")
	}

	emptyHooks := config.Hooks{}
	if emptyHooks.HasHooks() {
		t.Error("HasHooks should be false for empty hooks")
	}
}
