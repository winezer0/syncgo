package transport

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHookFuncImplementsSyncHook verifies HookFunc satisfies SyncHook interface.
func TestHookFuncImplementsSyncHook(t *testing.T) {
	var _ SyncHook = HookFunc{}
	var _ SyncHook = HookFunc{
		OnStart:    func(name string, total int) error { return nil },
		OnStart_:   func(path string, size int64) error { return nil },
		OnProgress: func(path string, sent, total int64) {},
		OnDone:     func(evt FileEvent) error { return nil },
		OnComplete: func(stats *SyncStats) error { return nil },
	}
}

// TestHookFuncNilSafe verifies nil function fields don't panic.
func TestHookFuncNilSafe(t *testing.T) {
	h := HookFunc{}
	if err := h.OnSyncStart("task", 5); err != nil {
		t.Errorf("OnSyncStart: %v", err)
	}
	if err := h.OnFileStart("file.txt", 100); err != nil {
		t.Errorf("OnFileStart: %v", err)
	}
	h.OnFileProgress("file.txt", 50, 100) // should not panic
	if err := h.OnFileDone(FileEvent{}); err != nil {
		t.Errorf("OnFileDone: %v", err)
	}
	if err := h.OnSyncDone(&SyncStats{}); err != nil {
		t.Errorf("OnSyncDone: %v", err)
	}
}

// TestHookFuncCallbacks verifies callbacks are invoked correctly.
func TestHookFuncCallbacks(t *testing.T) {
	var syncName string
	var syncTotal int
	var filePath string
	var fileSize int64
	var progressSent, progressTotal int64
	var doneEvt FileEvent
	var doneStats *SyncStats

	h := HookFunc{
		OnStart: func(name string, total int) error {
			syncName = name
			syncTotal = total
			return nil
		},
		OnStart_: func(path string, size int64) error {
			filePath = path
			fileSize = size
			return nil
		},
		OnProgress: func(path string, sent, total int64) {
			progressSent = sent
			progressTotal = total
		},
		OnDone: func(evt FileEvent) error {
			doneEvt = evt
			return nil
		},
		OnComplete: func(stats *SyncStats) error {
			doneStats = stats
			return nil
		},
	}

	h.OnSyncStart("deploy", 42)
	if syncName != "deploy" || syncTotal != 42 {
		t.Errorf("OnStart: name=%q total=%d", syncName, syncTotal)
	}

	h.OnFileStart("/app/main.go", 2048)
	if filePath != "/app/main.go" || fileSize != 2048 {
		t.Errorf("OnStart_: path=%q size=%d", filePath, fileSize)
	}

	h.OnFileProgress("/app/main.go", 1024, 2048)
	if progressSent != 1024 || progressTotal != 2048 {
		t.Errorf("OnProgress: sent=%d total=%d", progressSent, progressTotal)
	}

	evt := FileEvent{RelPath: "a.txt", IsNew: true, FileSize: 512}
	h.OnFileDone(evt)
	if doneEvt.RelPath != "a.txt" || !doneEvt.IsNew {
		t.Errorf("OnDone: %+v", doneEvt)
	}

	stats := &SyncStats{TotalFiles: 10, NewFiles: 3}
	h.OnSyncDone(stats)
	if doneStats.TotalFiles != 10 || doneStats.NewFiles != 3 {
		t.Errorf("OnSyncDone: %+v", doneStats)
	}
}

// TestScanLocalFilesBasic verifies ScanLocalFiles finds files correctly.
func TestScanLocalFilesBasic(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main"), 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", "c.txt"), []byte("nested"), 0644)

	files, err := ScanLocalFiles(dir, nil, false)
	if err != nil {
		t.Fatalf("ScanLocalFiles: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("found %d files, want 3", len(files))
	}
}

// TestScanLocalFilesExclude verifies exclude patterns work.
func TestScanLocalFilesExclude(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0644)
	os.WriteFile(filepath.Join(dir, "skip.log"), []byte("skip"), 0644)
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0755)
	os.WriteFile(filepath.Join(dir, "node_modules", "pkg.js"), []byte("x"), 0644)

	files, err := ScanLocalFiles(dir, []string{"*.log", "node_modules"}, false)
	if err != nil {
		t.Fatalf("ScanLocalFiles: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("found %d files, want 1 (only keep.txt)", len(files))
		for _, f := range files {
			t.Logf("  %s", f.Path)
		}
	}
}

// TestScanLocalFilesSkipDots verifies dot files are skipped.
func TestScanLocalFilesSkipDots(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("v"), 0644)
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("h"), 0644)
	os.MkdirAll(filepath.Join(dir, ".git"), 0755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("g"), 0644)

	files, err := ScanLocalFiles(dir, nil, true)
	if err != nil {
		t.Fatalf("ScanLocalFiles: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("found %d files, want 1 (only visible.txt)", len(files))
	}
}

// TestPackLocalTarGz verifies tar.gz packing works.
func TestPackLocalTarGz(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("content1"), 0644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0755)
	os.WriteFile(filepath.Join(dir, "subdir", "file2.txt"), []byte("content2"), 0644)

	archivePath, fileCount, totalSize, err := PackLocalTarGz(dir, nil, false, false)
	if err != nil {
		t.Fatalf("PackLocalTarGz: %v", err)
	}
	defer os.Remove(archivePath)

	if fileCount != 2 {
		t.Errorf("fileCount = %d, want 2", fileCount)
	}
	if totalSize != 16 { // "content1" + "content2" = 8+8
		t.Errorf("totalSize = %d, want 16", totalSize)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Errorf("archive not created: %v", err)
	}
}

// TestPackLocalTarGzFlat verifies flat mode omits source folder name.
func TestPackLocalTarGzFlat(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.go"), []byte("package main"), 0644)

	archivePath, fileCount, _, err := PackLocalTarGz(dir, nil, false, true)
	if err != nil {
		t.Fatalf("PackLocalTarGz flat: %v", err)
	}
	defer os.Remove(archivePath)

	if fileCount != 1 {
		t.Errorf("fileCount = %d, want 1", fileCount)
	}
}

// TestMatchProtect verifies protect pattern matching.
func TestMatchProtect(t *testing.T) {
	tests := []struct {
		path     string
		patterns []string
		want     bool
	}{
		{"/app/data.db", []string{"*.db"}, true},
		{"/app/main.go", []string{"*.db"}, false},
		{"/app/config", []string{"config"}, true},        // basename match
		{"/app/src/main.go", []string{"*.go"}, true},
		{"/app/readme.md", nil, false},
		{"/app/readme.md", []string{}, false},
		{"/app/logs", []string{"logs"}, true},            // basename match
		{"/app/data.db", []string{"/app/*.db"}, true},    // full path match
	}

	for _, tt := range tests {
		got := MatchProtect(tt.path, tt.patterns)
		if got != tt.want {
			t.Errorf("MatchProtect(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.want)
		}
	}
}

// TestEscapeShell verifies shell escaping.
func TestEscapeShell(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"it's", "it'\\''s"},
		{"path/to/file", "path/to/file"},
		{"a'b'c", "a'\\''b'\\''c"},
	}

	for _, tt := range tests {
		got := EscapeShell(tt.input)
		if got != tt.want {
			t.Errorf("EscapeShell(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestHookFailed verifies HookFailed detection.
func TestHookFailed(t *testing.T) {
	if HookFailed(nil) {
		t.Error("HookFailed(nil) should be false")
	}
	if HookFailed([]HookResult{{Command: "ok", Output: "done"}}) {
		t.Error("HookFailed with no errors should be false")
	}
	if !HookFailed([]HookResult{{Command: "fail", Err: os.ErrPermission}}) {
		t.Error("HookFailed with error should be true")
	}
}

// TestFormatHookResults verifies formatting doesn't panic.
func TestFormatHookResults(t *testing.T) {
	results := []HookResult{
		{Command: "echo hello", Output: "hello"},
		{Command: "false", Err: os.ErrNotExist},
	}
	out := FormatHookResults(results)
	if out == "" {
		t.Error("FormatHookResults returned empty string")
	}
}
