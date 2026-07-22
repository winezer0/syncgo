[简体中文](README.md) | English

# Shuttle — rsync-style delta sync for Windows

**Shuttle** is a Windows-native file sync tool. Define mappings in `syncd.yaml` — one command to push. Powered by [go-rsync](https://github.com/henryborner/go-rsync) (standalone rsync delta library). Not wire-compatible with standard rsync (uses CHAR_OFFSET=31, custom wire protocol).

```powershell
shuttle                    # double-click to launch TUI
shuttle push web           # sync a task
shuttle exec vps "uptime"  # run remote command
```

## Features

- **Dual sync modes** — `overlay` (incremental) / `full_replace` (tar.gz pack & replace)
- **Delta transfer** — rsync algorithm, only changed blocks are transmitted
- **Task Hooks** — Run remote commands before/after sync (stop/start services, clear cache)
- **Remote exec** — `shuttle exec` for standalone SSH commands, no sync task needed
- **Agent auto-deploy** — `shuttle deploy-agent` cross-compiles & deploys remote agent
- **Auth types** — auto / password / private_key authentication
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
- **Single binary** — `shuttle.exe`, zero extra dependencies

## Install

Download from [Releases](https://github.com/winezer0/syncgo/releases):

- **`shuttle.exe`** — Windows main program
- **`shuttle_linux`** — Linux remote agent (deployed via `deploy-agent` or TUI)

## Quick Start

```powershell
.\shuttle.exe                        # double-click for TUI
.\shuttle.exe tui                    # TUI from terminal
.\shuttle.exe init                   # generate config template
.\shuttle.exe list                   # list tasks & servers
.\shuttle.exe test myserver          # test SSH connection
.\shuttle.exe push web               # sync
.\shuttle.exe push --dry-run         # preview changes
.\shuttle.exe deploy-agent myserver  # deploy remote delta agent
.\shuttle.exe exec myserver "df -h"  # run remote command
.\shuttle.exe exec --all "uptime"    # run on all servers
.\shuttle.exe version                # version info
```

> Double-click `shuttle.exe` to enter TUI and create config — no manual YAML editing needed.

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
    auth_type: auto        # auto / password / private_key
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
| `shuttle` | Double-click for TUI |
| `shuttle push [name]` | Sync tasks |
| `shuttle list` | List all tasks and servers |
| `shuttle config` | Config summary |
| `shuttle config --schema` | Full field reference |
| `shuttle test <server>` | Test SSH connection |
| `shuttle init` | Generate config template |
| `shuttle exec <server> "cmd"` | Run remote command |
| `shuttle exec --all "cmd"` | Run on all servers |
| `shuttle exec <server> --file script.sh` | Command from file |
| `shuttle deploy-agent <server>` | Deploy remote delta agent |
| `shuttle version` | Version info |

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
shuttle exec vps "ls -la /var/www"
shuttle exec vps "systemctl restart nginx"
shuttle exec vps "df -h && free -m"

# All servers
shuttle exec --all "uptime"

# Command from file
shuttle exec vps --file deploy.sh
```

## Agent Deployment

`shuttle deploy-agent` automates remote delta agent deployment:

```powershell
shuttle deploy-agent myserver
```

Steps performed:
1. Connect and detect CPU architecture (`uname -m`)
2. Cross-compile shuttle for Linux (amd64/arm64/arm/386/riscv64)
3. Upload binary to `~/.local/bin/shuttle` via SFTP
4. Set executable permission (`chmod 0755`)
5. Ensure `~/.local/bin` is in PATH
6. Verify deployment (`shuttle version`)

After deployment, `shuttle push` automatically uses delta transfers.

## Library Usage (Go SDK)

Shuttle can be embedded as a Go library:

```go
import "github.com/winezer0/syncgo/syncer"

// Programmatic creation (no YAML config needed)
s := syncer.New(syncer.Options{
    Host:     "192.168.1.100",
    Port:     22,
    User:     "deploy",
    AuthType: "private_key",
    KeyFile:  "~/.ssh/id_ed25519",
    Workers:  4,
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

The remote agent caches block signatures in `~/.shuttle_cache/`. When a file hasn't changed, the cached signature is returned without reading the file from disk — significantly speeding up repeat syncs.

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
