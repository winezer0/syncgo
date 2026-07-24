[简体中文](README.md) | English

# syncgo — cross-platform rsync-style delta sync

**syncgo** is a cross-platform (Windows / macOS / Linux) file sync tool. Define mappings in `syncd.yaml` — one command to push. Ships with a built-in [`delta`](delta/) package (derived from [go-rsync](https://github.com/henryborner/go-rsync)) implementing the rsync delta algorithm. Not wire-compatible with standard rsync (uses CHAR_OFFSET=31, custom wire protocol). Pure Go + Go assembly, compiled with `CGO_ENABLED=0` into fully static binaries with no external dependencies.

> Original project: [shuttle](https://github.com/henryborner/shuttle)

```powershell
syncgo                    # double-click to launch TUI
syncgo push web           # sync a task
syncgo exec vps "uptime"  # run remote command
```

## Features

- **Cross-platform** — Windows / macOS / Linux, amd64 / arm64 (Apple Silicon, AWS Graviton, Raspberry Pi)
- **Pure Go build** — `CGO_ENABLED=0` static binaries, no CGO, no libc; conditional TUI compilation (`-tags tui`), lite build only ~8.6 MB
- **Dual sync modes** — `overlay` (incremental) / `full_replace` (tar.gz pack & replace)
- **Delta transfer** — rsync algorithm, only changed blocks are transmitted
- **Task Hooks** — Run remote commands before/after sync (stop/start services, clear cache)
- **Remote exec** — `syncgo exec` for standalone SSH commands, no sync task needed
- **Agent auto-deploy** — `syncgo deploy-agent` with three-level fallback (local file → release download → cross-compile) + remote execution verification
- **Auth** — password if configured, key_file if configured, otherwise auto-detects ~/.ssh keys
- **Retry policy** — Configurable max retries and delay for transient failures
- **Incremental toggle** — Global + per-task incremental switch
- **Per-server protect** — Glob patterns, matching remote files never overwritten/deleted
- **Conflict detection** — Detects when remote files are newer than local
- **TUI** — Dashboard, mappings, servers, explorer, settings
- **SFTP/SSH** — Local → remote with auto key detection
- **mmap** — Memory-mapped I/O for large file comparison
- **Signature cache** — Remote agent caches block signatures, skips disk reads on repeat syncs
- **Bilingual** — EN/ZH toggle in settings
- **Library API** — Embeddable as a Go library (`syncer` package)
- **Single binary** — Zero extra dependencies

## Install

Download the binary for your platform from [Releases](https://github.com/winezer0/syncgo/releases):

- **`syncgo.exe`** — Windows main program
- **`syncgo_linux`** — Linux remote agent (deployed via `deploy-agent` or TUI)

### Build from Source

Requires Go 1.26+. Pure Go implementation — no C compiler needed:

```bash
git clone https://github.com/winezer0/syncgo.git
cd syncgo

# Lite build (default, no TUI, ~8.6 MB)
CGO_ENABLED=0 go build -ldflags="-s -w" -o syncgo ./cmd/syncgo

# Full build (with interactive TUI, ~9.6 MB)
CGO_ENABLED=0 go build -tags tui -ldflags="-s -w" -o syncgo ./cmd/syncgo

# Cross-compile examples (remote agents don't need TUI)
CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -ldflags="-s -w" -o syncgo_linux_amd64  ./cmd/syncgo
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -tags tui -ldflags="-s -w" -o syncgo_darwin_arm64 ./cmd/syncgo
```

**Build variants:**

| Variant | Build Tag | Size | Use Case |
|---------|-----------|------|----------|
| Lite | default (no tag) | ~8.6 MB | Remote agent, CLI automation, servers |
| Full | `-tags tui` | ~9.6 MB | Desktop interaction, double-click TUI management |

> The lite build excludes the interactive TUI (bubbletea/lipgloss). All CLI commands (push, exec, deploy-agent, etc.) work normally.  
> Remote agents only need the `receive` subcommand — the lite build saves ~1 MB transfer size.

Supported targets: `windows`, `darwin`, `linux` × `amd64`, `arm64` (`deploy-agent` additionally supports `arm`/`386`/`riscv64` for remote agents).

## Quick Start

```powershell
.\syncgo.exe                        # double-click for TUI
.\syncgo.exe tui                    # TUI from terminal
.\syncgo.exe init                   # generate config template
.\syncgo.exe list                   # list tasks & servers
.\syncgo.exe test myserver          # test SSH connection
.\syncgo.exe push web               # sync
.\syncgo.exe push --dry-run         # preview changes
.\syncgo.exe deploy-agent myserver  # deploy remote delta agent
.\syncgo.exe exec myserver "df -h"  # run remote command
.\syncgo.exe exec --all "uptime"    # run on all servers
.\syncgo.exe version                # version info
```

> Double-click `syncgo.exe` to enter TUI and create config — no manual YAML editing needed.

## Config

```yaml
# syncd.yaml
version: "1.0"
language: en               # en / zh
checksum: xxh64            # md5 / sha256 / xxh64 / xxh3
workers: 4                 # delta parallel workers: 1=serial, 2/4/8=parallel
incremental: true          # global incremental toggle

retry:                     # retry policy
  max_retries: 3
  delay_ms: 1000

servers:
  - name: myserver
    host: 192.168.1.100
    port: 22
    user: deploy
    key_file: ~/.ssh/id_ed25519
    protect:               # protect patterns (glob)
      - "*.db"
      - "*.pem"
      - ".env"

tasks:
  # Incremental overlay (default mode)
  - name: web
    source: E:\projects\web\dist\
    target: myserver:/var/www/html/
    options:
      sync_mode: overlay
      delete: true
      exclude: ["*.tmp", ".git/"]
      checksum: false
      flat: false
    hooks:
      before:
        - "systemctl stop nginx"
      after:
        - "systemctl start nginx"
      on_error: abort      # abort / warn / ignore

  # Full replace (clean release)
  - name: app-release
    source: E:\builds\app\
    target: myserver:/opt/app/
    options:
      sync_mode: full_replace
      exclude: ["*.log"]

  # Single file sync
  - name: nginx-config
    source: E:\configs\nginx.conf
    target: myserver:/etc/nginx/nginx.conf
    options:
      checksum: true
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `syncgo` | Double-click for TUI |
| `syncgo push [name]` | Sync tasks |
| `syncgo list` | List all tasks and servers |
| `syncgo config` | Config summary |
| `syncgo config --schema` | Full field reference |
| `syncgo test <server>` | Test SSH connection |
| `syncgo init` | Generate config template |
| `syncgo exec <server> "cmd"` | Run remote command |
| `syncgo exec --all "cmd"` | Run on all servers |
| `syncgo exec <server> --file script.sh` | Command from file |
| `syncgo deploy-agent <server>` | Deploy remote delta agent |
| `syncgo version` | Version info |

### push Flags

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview only, no changes |
| `-v` | Verbose output (includes delta-matched bytes) |
| `-w N` | Parallel workers (default 4) |
| `--algo md5\|xxh64\|xxh3\|sha256` | Checksum algorithm |
| `-c path` | Config file path |

## Sync Modes

### overlay (incremental, default)

- Upload only new/modified files
- Preserve remote-only files (configs, keys, logs)
- Optional delete of orphans (`delete: true`)
- Supports rsync delta incremental transfer
- Use case: iterative deployment, preserve remote configs

### full_replace (tar.gz pack & replace)

- Pack local directory into tar.gz
- Upload archive to remote `/tmp`
- Clean target directory + extract archive
- Remote-only files are **DELETED** (strong consistency)
- Use case: clean release, binary update, environment reset
- Requires: remote server must have `tar` command

## Task Hooks (Remote Commands)

Automatically run remote SSH commands before/after sync:

```yaml
tasks:
  - name: web
    source: E:\projects\dist\
    target: myserver:/var/www/html/
    hooks:
      before:
        - "systemctl stop nginx"       # stop service before sync
        - "cp -r /var/www /tmp/backup" # backup
      after:
        - "systemctl start nginx"      # restart after sync
        - "rm -rf /tmp/cache/*"        # clear cache
      on_error: abort                  # abort / warn / ignore
```

**Execution rules:**
- `before` hooks run after SSH connect, before any file transfer
- `after` hooks run only if sync succeeded
- Commands run sequentially in listed order
- Use full paths for binaries (SSH exec may not load your full PATH)

## Remote Command Execution

```powershell
# Single server
syncgo exec vps "ls -la /var/www"
syncgo exec vps "systemctl restart nginx"
syncgo exec vps "df -h && free -m"

# All servers
syncgo exec --all "uptime"

# Command from file
syncgo exec vps --file deploy.sh
```

## Agent Deployment

`syncgo deploy-agent` automates remote delta agent deployment:

```powershell
syncgo deploy-agent myserver
```

### Agent = Main Binary

The agent is **the same binary** as the main program (same source: `cmd/syncgo`). The remote only uses the `receive` subcommand for delta signature computation and file reconstruction, but the deployed binary contains all functionality.

### Binary Resolution (Three-Level Fallback)

| Priority | Source | Description |
|----------|--------|-------------|
| 1 | Local file | `syncgo_linux_<arch>` in the program directory (e.g. `syncgo_linux_amd64`) |
| 2 | GitHub Releases | Auto-download `v<version>/syncgo_linux_<arch>` |
| 3 | Cross-compile | Local `go build` (requires Go toolchain, `CGO_ENABLED=0` static build) |

> Tip: Place pre-built `syncgo_linux_amd64` / `syncgo_linux_arm64` alongside `syncgo.exe` for offline deployment — no network or Go environment needed.

### Steps Performed

1. Connect and detect CPU architecture (`uname -m`)
2. Resolve agent binary via three-level fallback
3. Upload binary to `~/.local/bin/syncgo` via SFTP
4. Set executable permission (`chmod 0755`)
5. Ensure `~/.local/bin` is in PATH
6. **Execution verification** — run `syncgo version` on remote to confirm it works
7. **Shared library diagnostics** — if execution fails, auto-run `ldd` to detect missing `.so` files and suggest fixes

### Verification Failure Example

```
  Verifying agent... failed: remote exec: exit status 127

  ⚠ Missing shared libraries on remote:
    libpthread.so.0 => not found
    libc.so.6 => not found

  The agent binary requires dynamic libraries not available on this system.
  Solution: rebuild with CGO_ENABLED=0 for a fully static binary.
```

> Binaries built with `CGO_ENABLED=0` are fully statically linked with no `.so` dependencies. This diagnostic primarily guards against manually placed dynamically-linked binaries.

After deployment, `syncgo push` automatically uses delta transfers.

## Library Usage (Go SDK)

syncgo can be embedded as a Go library:

```go
import "github.com/winezer0/syncgo/syncer"

// Programmatic creation (no YAML config needed)
s := syncer.New(syncer.Options{
    Host:    "192.168.1.100",
    Port:    22,
    User:    "deploy",
    KeyFile: "~/.ssh/id_ed25519",
    Workers: 4,
})
defer s.Close()

// Upload a single file
s.UploadFile("local.conf", "/etc/app/config.conf")

// Sync directory (incremental overlay)
s.UploadDir("./dist", "/var/www/html")

// Sync directory (full replace + delete orphans)
s.UploadDirWithOptions("./build", "/opt/app", syncer.SyncDirOptions{
    Mode:   "full_replace",
    Delete: true,
})

// Execute remote command
out, _ := s.ExecRemote("systemctl restart nginx")

// Download remote directory
s.DownloadDir("/var/log/app", "./logs")
```

### Context Support

All public methods have `*Context` variants supporting cancellation and timeouts:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

err := s.ConnectContext(ctx)
err = s.UploadDirContext(ctx, "./dist", "/var/www/html")
out, err := s.ExecRemoteContext(ctx, "df -h")
```

### Functional Hooks

```go
import "github.com/winezer0/syncgo/transport"

s.SetHook(&transport.HookFunc{
    OnStart: func(taskName string, totalFiles int) error {
        fmt.Printf("Syncing %s: %d files\n", taskName, totalFiles)
        return nil
    },
    OnProgress: func(path string, sent, total int64) {
        fmt.Printf("\r%s: %d%%", path, sent*100/total)
    },
    OnDone: func(evt transport.FileEvent) error {
        if evt.Error != nil {
            log.Printf("Failed: %s: %v", evt.RelPath, evt.Error)
        }
        return nil
    },
})
```

## Shortcuts (TUI)

| Key | Action |
|-----|--------|
| `Enter` | Sync selected |
| `A` `E` `D` | Add/Edit/Delete mapping |
| `R` | Sync current mapping |
| `Ctrl+T` | Test server connection |
| `P` | Edit protect list |
| `Tab` | Toggle file browser |

## How It Works

### Delta Transfer (rsync algorithm)

1. **Chunking** — Source file is split into fixed-size blocks
2. **Signatures** — Two checksums per block: fast rolling checksum + strong checksum (xxh64/md5/sha256)
3. **Matching** — Remote agent slides a window over its file copy to find matching blocks
4. **Delta** — Only non-matching byte sequences (literals) are transmitted; matching blocks referenced by index
5. **Reconstruction** — Remote rebuilds the file from delta instructions (atomic rename)

If files are identical, only the signature list (a few KB) is transferred — no file data moves.

### Smart Tiering

```
Agent available → delta incremental transfer (only changed blocks)
No agent       → automatic fallback to full SFTP upload
```

### Signature Cache

The remote agent caches block signatures in `~/.syncgo_cache/`. When a file hasn't changed, the cached signature is returned without reading the file from disk — significantly speeding up repeat syncs.

### Wire Protocol

- **CHAR_OFFSET = 31**: character offset parameter
- **Default strong checksum = xxh64**: 64-bit xxHash
- Supports md5 (128-bit), xxh3 (128-bit), sha256 (256-bit)

### Server Protection

Glob-pattern protect list. Matching remote files are **never overwritten or deleted**. Useful for safeguarding databases, certificates, config files, and other critical data.

### Permission Requirements

| Operation | Required Permission |
|-----------|-------------------|
| File listing / signature computation | Directory `r-x`, file `r--` |
| Upload / delta reconstruction | Target directory `w` permission |
| Delete files | Parent directory `w` permission |
| Agent deploy / cache | User HOME write permission |

> Recommendation: Target directory owned by the SSH login user. Normal user permissions are sufficient for all operations.

## License

MIT
