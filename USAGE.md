# SyncGo Library Usage Guide

SyncGo can be used as a Go library for embedding file synchronization capabilities into your own applications.

## Installation

```bash
go get github.com/winezer0/syncgo
```

## Quick Start

### Basic File Upload

```go
package main

import (
    "context"
    "log"

    "github.com/winezer0/syncgo/syncer"
)

func main() {
    // Create a syncer with programmatic options
    s := syncer.New(syncer.Options{
        Host:     "192.168.1.100",
        Port:     22,
        User:     "deploy",
        Password: "secret",
        // Or use key file:
        // KeyFile: "/path/to/private/key",
    })
    defer s.Close()

    // Upload a single file
    ctx := context.Background()
    if err := s.UploadFileContext(ctx, "/local/path/file.txt", "/remote/path/file.txt"); err != nil {
        log.Fatal(err)
    }
}
```

### Directory Synchronization

```go
package main

import (
    "context"
    "log"

    "github.com/winezer0/syncgo/syncer"
)

func main() {
    s := syncer.New(syncer.Options{
        Host:     "example.com",
        User:     "deploy",
        KeyFile:  "~/.ssh/id_ed25519",
        Workers:  4, // delta parallel workers
    })
    defer s.Close()

    ctx := context.Background()

    // Simple directory sync (overlay mode)
    if err := s.UploadDirContext(ctx, "/local/project", "/remote/project"); err != nil {
        log.Fatal(err)
    }

    // Advanced: with explicit options
    opts := syncer.SyncDirOptions{
        Mode:     "overlay",    // or "full_replace"
        Delete:   true,         // delete remote files not in local
        Checksum: true,         // use checksum comparison
        Exclude:  []string{"*.log", ".git/*"},
        Workers:  8,
    }
    if err := s.UploadDirWithOptionsContext(ctx, "/local/project", "/remote/project", opts); err != nil {
        log.Fatal(err)
    }
}
```

### Deploy Delta Agent

Enable rsync-style delta transfers by deploying the syncgo agent to the remote server:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/winezer0/syncgo/config"
    "github.com/winezer0/syncgo/syncer"
)

func main() {
    // Method 1: Using existing Syncer connection
    s := syncer.New(syncer.Options{
        Host:     "example.com",
        User:     "deploy",
        Password: "secret",
    })
    defer s.Close()

    ctx := context.Background()
    opts := syncer.DeployAgentOptions{
        Version: config.DefaultVersion, // or specify: "0.0.3"
        Progress: func(msg string) {
            fmt.Println("  " + msg)
        },
    }

    if err := s.DeployAgent(ctx, opts); err != nil {
        log.Fatal(err)
    }
    fmt.Println("Agent deployed successfully!")

    // Method 2: Standalone deployment (no existing connection needed)
    server := config.Server{
        Host: "example.com",
        Port: 22,
        User: "deploy",
        Pass: "secret",
    }
    if err := syncer.DeployAgentStandalone(ctx, server, opts); err != nil {
        log.Fatal(err)
    }
}
```

### Remote Command Execution

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/winezer0/syncgo/config"
    "github.com/winezer0/syncgo/syncer"
)

func main() {
    s := syncer.New(syncer.Options{
        Host:    "example.com",
        User:    "deploy",
        KeyFile: "~/.ssh/id_ed25519",
    })
    defer s.Close()

    ctx := context.Background()

    // Execute a single command
    output, err := s.ExecRemoteContext(ctx, "uname -a")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Remote system: %s\n", output)

    // Execute hooks (before/after commands)
    hooks := config.Hooks{
        Before: []string{
            "systemctl stop myapp",
            "backup.sh",
        },
        After: []string{
            "systemctl start myapp",
        },
        OnError: "abort", // or "warn", "ignore"
    }
    results, err := s.ExecHooksContext(ctx, hooks)
    if err != nil {
        log.Fatal(err)
    }
    for _, r := range results {
        fmt.Printf("[%s] %s: %s\n", r.Status, r.Command, r.Output)
    }
}
```

### Download Files from Remote

```go
package main

import (
    "context"
    "log"

    "github.com/winezer0/syncgo/syncer"
)

func main() {
    s := syncer.New(syncer.Options{
        Host:     "example.com",
        User:     "deploy",
        Password: "secret",
    })
    defer s.Close()

    ctx := context.Background()

    // Download a single file
    reader, err := s.GetFile("/remote/path/file.txt")
    if err != nil {
        log.Fatal(err)
    }
    defer reader.Close()
    // ... read from reader ...

    // Download entire directory
    if err := s.DownloadDirContext(ctx, "/remote/project", "/local/backup"); err != nil {
        log.Fatal(err)
    }
}
```

### Using YAML Configuration

If you prefer YAML-based configuration:

```go
package main

import (
    "context"
    "log"

    "github.com/winezer0/syncgo/config"
    "github.com/winezer0/syncgo/syncer"
)

func main() {
    // Load configuration from YAML file
    cfg, err := config.Load("syncd.yaml")
    if err != nil {
        log.Fatal(err)
    }

    // Create syncer from config and server name
    s, err := syncer.NewSyncer(*cfg, "production")
    if err != nil {
        log.Fatal(err)
    }
    defer s.Close()

    ctx := context.Background()

    // Execute a task defined in the YAML
    task := cfg.Tasks[0] // or find by name
    stats, err := s.SyncTaskContext(ctx, task, false)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Synced %d files, %d errors", stats.FilesSynced, len(stats.Errors))
}
```

### Progress Reporting

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/winezer0/syncgo/syncer"
    "github.com/winezer0/syncgo/transport"
)

// Implement the SyncHook interface
type ProgressHook struct{}

func (h *ProgressHook) OnSyncStart(taskName string, totalFiles int) error {
    fmt.Printf("Starting sync: %s (%d files)\n", taskName, totalFiles)
    return nil
}

func (h *ProgressHook) OnFileStart(path string, size int64) error {
    fmt.Printf("Uploading: %s (%d bytes)\n", path, size)
    return nil
}

func (h *ProgressHook) OnFileProgress(path string, offset int64) error {
    fmt.Printf("Progress: %s @ %d bytes\n", path, offset)
    return nil
}

func (h *ProgressHook) OnFileComplete(path string, size int64) error {
    fmt.Printf("Complete: %s\n", path)
    return nil
}

func (h *ProgressHook) OnSyncComplete(stats *transport.SyncStats) error {
    fmt.Printf("Sync complete: %d files, %d errors\n", stats.FilesSynced, len(stats.Errors))
    return nil
}

func main() {
    s := syncer.New(syncer.Options{
        Host:     "example.com",
        User:     "deploy",
        Password: "secret",
    })
    defer s.Close()

    // Set the progress hook
    s.SetHook(&ProgressHook{})

    ctx := context.Background()
    if err := s.UploadDirContext(ctx, "/local/project", "/remote/project"); err != nil {
        log.Fatal(err)
    }
}
```

## API Reference

### Core Types

- **`syncer.Options`** — Programmatic configuration (no YAML needed)
- **`syncer.Syncer`** — Main facade for all sync operations
- **`syncer.SyncDirOptions`** — Advanced directory sync configuration
- **`syncer.DeployAgentOptions`** — Agent deployment configuration

### Connection Methods

- **`syncer.New(opts Options) *Syncer`** — Create from programmatic options
- **`syncer.NewSyncer(cfg config.Config, serverName string) (*Syncer, error)`** — Create from YAML config
- **`(s *Syncer) Connect() error`** — Establish connection
- **`(s *Syncer) ConnectContext(ctx context.Context) error`** — Connect with context

### File Operations

- **`(s *Syncer) UploadFile(local, remote string) error`** — Upload single file
- **`(s *Syncer) UploadDir(local, remote string) error`** — Sync directory (overlay mode)
- **`(s *Syncer) UploadDirWithOptions(local, remote string, opts SyncDirOptions) error`** — Advanced directory sync
- **`(s *Syncer) DownloadDir(remote, local string) error`** — Download directory
- **`(s *Syncer) GetFile(remote string) (io.ReadCloser, error)`** — Download single file

### Remote Execution

- **`(s *Syncer) ExecRemote(command string) (string, error)`** — Execute command
- **`(s *Syncer) ExecHooks(hooks config.Hooks) ([]transport.HookResult, error)`** — Execute hooks

### Agent Deployment

- **`(s *Syncer) DeployAgent(ctx context.Context, opts DeployAgentOptions) error`** — Deploy agent (reuses connection)
- **`syncer.DeployAgentStandalone(ctx, server, opts) error`** — Deploy agent (standalone)

### Utility

- **`(s *Syncer) SetHook(h transport.SyncHook)`** — Set progress reporting hook
- **`(s *Syncer) Close()`** — Release all resources
- **`(s *Syncer) IsConnected() bool`** — Check connection status

All methods have `*Context` variants that accept `context.Context` for cancellation and timeout support.

## Sync Modes

### Overlay Mode (default)
Incremental sync — uploads new/modified files, optionally deletes remote-only files.

```go
opts := syncer.SyncDirOptions{
    Mode:   "overlay",
    Delete: true, // remove remote files not in local
}
```

### Full Replace Mode
Packs directory into tar.gz, uploads, extracts, and cleans remote orphans. Faster for large directories with many changes.

```go
opts := syncer.SyncDirOptions{
    Mode: "full_replace",
}
```

## Delta Transfers

Delta transfers require the syncgo agent deployed on the remote server. Once deployed, only changed portions of files are transferred, significantly reducing bandwidth usage.

```go
// Deploy agent first
s.DeployAgent(ctx, syncer.DeployAgentOptions{})

// Subsequent syncs automatically use delta transfers
s.UploadDir(ctx, "/local", "/remote")
```

Agent binary resolution order:
1. Explicit `BinaryPath` in options
2. Local pre-built binary (`syncgo_linux_<arch>` in exe/CWD directory)
3. Download from GitHub Releases
4. Cross-compile from source (requires Go toolchain + project root)

## Error Handling

All methods return errors that can be inspected for specific failure modes:

```go
if err := s.UploadFileContext(ctx, local, remote); err != nil {
    if strings.Contains(err.Error(), "connect cancelled") {
        // Context was cancelled
    } else if strings.Contains(err.Error(), "upload failed after") {
        // Retry exhausted
    } else {
        // Other error
    }
    log.Fatal(err)
}
```

## Examples

See the `syncer/deploy_agent_test.go` and `syncer/syncer_test.go` files for more usage examples.
