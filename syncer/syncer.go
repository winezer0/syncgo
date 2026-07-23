// Package syncer provides a high-level public API for file synchronization.
// It wraps the transport and sync engine into a simple facade suitable for
// embedding in third-party Go projects.
//
// syncer 包提供文件同步的高层公开 API。
// 将 transport 和 sync engine 封装为简洁门面，适合嵌入第三方 Go 项目。
package syncer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/winezer0/syncgo/config"
	"github.com/winezer0/syncgo/transport"
)

// Options configures a Syncer programmatically without a YAML config file.
// Options 以编程方式配置 Syncer，无需 YAML 配置文件。
type Options struct {
	Host       string   // remote host / 远端主机
	Port       int      // SSH port (default 22) / SSH 端口（默认22）
	User       string   // SSH user / SSH 用户
	KeyFile    string   // SSH private key path / SSH 私钥路径
	Password   string   // SSH password / SSH 密码
	Protect    []string // protect patterns / 保护模式
	Workers    int      // delta parallel workers (default 4) / delta 并行数（默认4）
	MaxRetries int      // max retry attempts (default 3) / 最大重试次数（默认3）
	DelayMs    int      // retry delay in milliseconds (default 1000) / 重试间隔毫秒（默认1000）
}

// New creates a Syncer from programmatic options (no YAML config needed).
// New 从编程式选项创建 Syncer（无需 YAML 配置）。
func New(opts Options) *Syncer {
	if opts.Port == 0 {
		opts.Port = 22
	}
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 3
	}
	if opts.DelayMs <= 0 {
		opts.DelayMs = 1000
	}
	cfg := config.Config{
		Workers: opts.Workers,
	}
	server := config.Server{
		Host:    opts.Host,
		Port:    opts.Port,
		User:    opts.User,
		KeyFile: opts.KeyFile,
		Pass:    opts.Password,
		Protect: opts.Protect,
	}
	return &Syncer{
		cfg:    cfg,
		server: server,
		retry: config.RetryConfig{
			MaxRetries: opts.MaxRetries,
			DelayMs:    opts.DelayMs,
		},
	}
}

// Syncer is the public facade for file synchronization operations.
// Syncer 文件同步操作的公开门面。
type Syncer struct {
	cfg       config.Config
	server    config.Server
	tr        *transport.SFTPTransport
	retryable *transport.RetryableTransport
	engine    *transport.SyncEngine
	retry     config.RetryConfig
	connected bool
}

// NewSyncer creates a new Syncer from a full config and a server name.
// The server must exist in cfg.Servers.
// NewSyncer 从完整配置和服务器名称创建 Syncer。
func NewSyncer(cfg config.Config, serverName string) (*Syncer, error) {
	server := cfg.GetServer(serverName)
	if server == nil {
		return nil, fmt.Errorf("server not found: %s", serverName)
	}
	return NewSyncerWithServer(cfg, *server), nil
}

// NewSyncerWithServer creates a new Syncer from a config and an explicit server.
// NewSyncerWithServer 从配置和显式服务器创建 Syncer。
func NewSyncerWithServer(cfg config.Config, server config.Server) *Syncer {
	retry := cfg.GetRetry()
	return &Syncer{
		cfg:    cfg,
		server: server,
		retry:  retry,
	}
}

// Connect establishes the SSH/SFTP connection with retry support.
// Connect 建立 SSH/SFTP 连接（带重试）。
func (s *Syncer) Connect() error {
	return s.ConnectContext(context.Background())
}

// ConnectContext establishes the SSH/SFTP connection with context and retry support.
// ConnectContext 使用 context 建立 SSH/SFTP 连接（带重试）。
func (s *Syncer) ConnectContext(ctx context.Context) error {
	if s.tr == nil {
		port := s.server.Port
		if port == 0 {
			port = 22
		}
		s.tr = transport.NewSFTP(transport.SFTPConfig{
			Host:    s.server.Host,
			Port:    port,
			User:    s.server.User,
			KeyFile: s.server.KeyFile,
			Pass:    s.server.Pass,
		})
		s.retryable = transport.NewRetryableTransport(s.tr, transport.RetryPolicy{
			MaxRetries: s.retry.MaxRetries,
			Delay:      time.Duration(s.retry.DelayMs) * time.Millisecond,
		})
		s.engine = transport.NewSyncEngine(s.tr)
	}

	var lastErr error
	for attempt := 0; attempt <= s.retry.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("connect cancelled: %w", err)
		}
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("connect cancelled: %w", ctx.Err())
			case <-time.After(time.Duration(s.retry.DelayMs) * time.Millisecond):
			}
		}

		// Run Connect in a goroutine so context cancellation can interrupt it.
		connErr := make(chan error, 1)
		go func() { connErr <- s.tr.Connect() }()

		select {
		case <-ctx.Done():
			return fmt.Errorf("connect cancelled: %w", ctx.Err())
		case err := <-connErr:
			if err != nil {
				lastErr = err
				continue
			}
			s.connected = true
			return nil
		}
	}
	return fmt.Errorf("connect to %s@%s:%d failed after %d attempts: %w",
		s.server.User, s.server.Host, s.server.Port, s.retry.MaxRetries+1, lastErr)
}

// UploadFile uploads a single local file to the remote path with retry.
// UploadFile 上传单个本地文件到远端路径（带重试）。
func (s *Syncer) UploadFile(local, remote string) error {
	return s.UploadFileContext(context.Background(), local, remote)
}

// UploadFileContext uploads a single local file with context support.
// UploadFileContext 使用 context 上传单个本地文件。
func (s *Syncer) UploadFileContext(ctx context.Context, local, remote string) error {
	if !s.connected {
		if err := s.ConnectContext(ctx); err != nil {
			return err
		}
	}

	var lastErr error
	for attempt := 0; attempt <= s.retry.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("upload cancelled: %w", err)
		}
		if attempt > 0 {
			s.tr.Reconnect()
			select {
			case <-ctx.Done():
				return fmt.Errorf("upload cancelled: %w", ctx.Err())
			case <-time.After(time.Duration(s.retry.DelayMs) * time.Millisecond):
			}
		}

		info, err := os.Stat(local)
		if err != nil {
			return fmt.Errorf("stat local file: %w", err)
		}
		f, err := os.Open(local)
		if err != nil {
			return fmt.Errorf("open local file: %w", err)
		}

		err = s.tr.PutFile(remote, f, info.Size())
		f.Close()
		if err != nil {
			lastErr = err
			if !s.tr.IsConnected() {
				continue
			}
			return fmt.Errorf("upload file: %w", err)
		}
		s.tr.SetModTime(remote, info.ModTime())
		return nil
	}
	return fmt.Errorf("upload file failed after %d attempts: %w", s.retry.MaxRetries+1, lastErr)
}

// UploadDir synchronizes a local directory to the remote path.
// UploadDir 将本地目录同步到远端路径。
func (s *Syncer) UploadDir(local, remote string) error {
	return s.UploadDirWithOptionsContext(context.Background(), local, remote, SyncDirOptions{})
}

// UploadDirContext synchronizes a local directory with context support.
// UploadDirContext 使用 context 同步本地目录。
func (s *Syncer) UploadDirContext(ctx context.Context, local, remote string) error {
	return s.UploadDirWithOptionsContext(ctx, local, remote, SyncDirOptions{})
}

// SyncDirOptions configures directory sync behavior.
// SyncDirOptions 配置目录同步行为。
type SyncDirOptions struct {
	Mode        config.SyncMode // overlay (default) or full_replace
	Delete      bool            // delete remote orphans (overlay mode)
	Exclude     []string        // exclude patterns
	Checksum    bool            // use checksum comparison
	Flat        bool            // flat mapping (no source folder name)
	ShowDots    bool            // include dot files
	Incremental *bool           // nil = use global config default
	DryRun      bool            // preview only
	Workers     int             // delta parallel workers
}

// UploadDirWithOptions synchronizes a local directory with explicit options.
// UploadDirWithOptions 使用显式选项同步本地目录。
func (s *Syncer) UploadDirWithOptions(local, remote string, opts SyncDirOptions) error {
	return s.UploadDirWithOptionsContext(context.Background(), local, remote, opts)
}

// UploadDirWithOptionsContext synchronizes a local directory with explicit options and context.
// UploadDirWithOptionsContext 使用显式选项和 context 同步本地目录。
func (s *Syncer) UploadDirWithOptionsContext(ctx context.Context, local, remote string, opts SyncDirOptions) error {
	if !s.connected {
		if err := s.ConnectContext(ctx); err != nil {
			return err
		}
	}

	mode := opts.Mode
	if mode == "" {
		mode = config.SyncOverlayPatch
	}

	incremental := s.cfg.IsIncremental()
	if opts.Incremental != nil {
		incremental = *opts.Incremental
	}

	syncOpts := transport.SyncOptions{
		Source:   local,
		Target:   remote,
		Delete:   opts.Delete,
		Exclude:  opts.Exclude,
		Protect:  s.server.Protect,
		Checksum: opts.Checksum,
		DryRun:   opts.DryRun,
		SkipDots: !opts.ShowDots,
		Workers:  opts.Workers,
		Flat:     opts.Flat,
	}

	if syncOpts.Workers <= 0 {
		syncOpts.Workers = s.cfg.Workers
		if syncOpts.Workers <= 0 {
			syncOpts.Workers = 4
		}
	}

	var lastErr error
	for attempt := 0; attempt <= s.retry.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("sync cancelled: %w", err)
		}
		if attempt > 0 {
			s.tr.Reconnect()
			select {
			case <-ctx.Done():
				return fmt.Errorf("sync cancelled: %w", ctx.Err())
			case <-time.After(time.Duration(s.retry.DelayMs) * time.Millisecond):
			}
		}

		var stats *transport.SyncStats
		var err error

		switch mode {
		case config.SyncFullReplace:
			stats, err = s.engine.SyncFullReplace(syncOpts)
		default:
			if !incremental {
				syncOpts.Delete = true
				syncOpts.Checksum = true
			}
			stats, err = s.engine.Sync(syncOpts)
		}

		if err != nil {
			lastErr = err
			if !s.tr.IsConnected() {
				continue
			}
			return fmt.Errorf("sync dir: %w", err)
		}

		if stats != nil && len(stats.Errors) > 0 {
			return fmt.Errorf("sync completed with %d errors, first: %w", len(stats.Errors), stats.Errors[0])
		}
		return nil
	}
	return fmt.Errorf("sync dir failed after %d attempts: %w", s.retry.MaxRetries+1, lastErr)
}

// SyncTask executes a named task from the config.
// SyncTask 执行配置中的命名任务。
func (s *Syncer) SyncTask(task config.Task, dryRun bool) (*transport.SyncStats, error) {
	return s.SyncTaskContext(context.Background(), task, dryRun)
}

// SyncTaskContext executes a named task with context support.
// SyncTaskContext 使用 context 执行命名任务。
func (s *Syncer) SyncTaskContext(ctx context.Context, task config.Task, dryRun bool) (*transport.SyncStats, error) {
	if !s.connected {
		if err := s.ConnectContext(ctx); err != nil {
			return nil, err
		}
	}

	_, remotePath := config.ParseTarget(task.Target)

	mode := task.Options.SyncMode
	if mode == "" {
		mode = config.SyncOverlayPatch
	}

	incremental := task.Options.IsIncremental(s.cfg.IsIncremental())

	syncOpts := transport.SyncOptions{
		Source:   task.Source,
		Target:   remotePath,
		Delete:   task.Options.Delete,
		Exclude:  task.Options.Exclude,
		Protect:  s.server.Protect,
		Checksum: task.Options.Checksum,
		DryRun:   dryRun,
		SkipDots: !task.Options.ShowDots,
		Workers:  s.cfg.Workers,
		Flat:     task.Options.Flat,
	}
	if syncOpts.Workers <= 0 {
		syncOpts.Workers = 4
	}

	var lastErr error
	for attempt := 0; attempt <= s.retry.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("sync task cancelled: %w", err)
		}
		if attempt > 0 {
			s.tr.Reconnect()
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("sync task cancelled: %w", ctx.Err())
			case <-time.After(time.Duration(s.retry.DelayMs) * time.Millisecond):
			}
		}

		var stats *transport.SyncStats
		var err error

		switch mode {
		case config.SyncFullReplace:
			stats, err = s.engine.SyncFullReplace(syncOpts)
		default:
			if !incremental {
				syncOpts.Delete = true
				syncOpts.Checksum = true
			}
			stats, err = s.engine.Sync(syncOpts)
		}

		if err != nil {
			lastErr = err
			if !s.tr.IsConnected() {
				continue
			}
			return nil, fmt.Errorf("sync task '%s': %w", task.Name, err)
		}
		return stats, nil
	}
	return nil, fmt.Errorf("sync task '%s' failed after %d attempts: %w", task.Name, s.retry.MaxRetries+1, lastErr)
}

// ExecRemote executes a command on the remote server and returns its output.
// ExecRemote 在远端服务器执行命令并返回输出。
func (s *Syncer) ExecRemote(command string) (string, error) {
	return s.ExecRemoteContext(context.Background(), command)
}

// ExecRemoteContext executes a remote command with context support.
// ExecRemoteContext 使用 context 在远端执行命令。
func (s *Syncer) ExecRemoteContext(ctx context.Context, command string) (string, error) {
	if !s.connected {
		if err := s.ConnectContext(ctx); err != nil {
			return "", err
		}
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("exec cancelled: %w", err)
	}
	return s.tr.ExecOutput(command)
}

// ExecHooks executes configured hooks on the remote server.
// Returns results for each command. Respects the OnError policy.
// ExecHooks 在远端服务器执行配置的钩子命令。
// 返回每个命令的结果，遵循 OnError 策略。
func (s *Syncer) ExecHooks(hooks config.Hooks) ([]transport.HookResult, error) {
	return s.ExecHooksContext(context.Background(), hooks)
}

// ExecHooksContext executes configured hooks with context support.
// ExecHooksContext 使用 context 执行配置的钩子命令。
func (s *Syncer) ExecHooksContext(ctx context.Context, hooks config.Hooks) ([]transport.HookResult, error) {
	if !s.connected {
		if err := s.ConnectContext(ctx); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("exec hooks cancelled: %w", err)
	}

	stopOnError := hooks.OnError == "" || hooks.OnError == config.HookAbort

	var allCommands []string
	allCommands = append(allCommands, hooks.Before...)
	allCommands = append(allCommands, hooks.After...)

	results := s.engine.ExecHooks(allCommands, stopOnError)

	if transport.HookFailed(results) && hooks.OnError == config.HookAbort {
		return results, fmt.Errorf("hook failed (abort policy)")
	}
	return results, nil
}

// DownloadDir downloads a remote directory to a local path recursively.
// DownloadDir 递归下载远端目录到本地路径。
func (s *Syncer) DownloadDir(remote, local string) error {
	return s.DownloadDirContext(context.Background(), remote, local)
}

// DownloadDirContext downloads a remote directory with context support.
// DownloadDirContext 使用 context 递归下载远端目录。
func (s *Syncer) DownloadDirContext(ctx context.Context, remote, local string) error {
	if !s.connected {
		if err := s.ConnectContext(ctx); err != nil {
			return err
		}
	}

	entries, err := s.tr.ListDirRecursive(remote)
	if err != nil {
		return fmt.Errorf("list remote dir: %w", err)
	}

	remoteSlash := strings.TrimRight(filepath.ToSlash(remote), "/")

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("download cancelled: %w", err)
		}

		relPath := strings.TrimPrefix(filepath.ToSlash(entry.Path), remoteSlash)
		relPath = strings.TrimLeft(relPath, "/")
		localPath := filepath.Join(local, filepath.FromSlash(relPath))

		if entry.IsDir {
			if err := os.MkdirAll(localPath, 0755); err != nil {
				return fmt.Errorf("mkdir %s: %w", localPath, err)
			}
			continue
		}

		// Ensure parent directory exists
		if dir := filepath.Dir(localPath); dir != "." {
			os.MkdirAll(dir, 0755)
		}

		rc, err := s.tr.GetFile(entry.Path)
		if err != nil {
			return fmt.Errorf("download %s: %w", entry.Path, err)
		}

		f, err := os.Create(localPath)
		if err != nil {
			rc.Close()
			return fmt.Errorf("create local file %s: %w", localPath, err)
		}

		_, copyErr := io.Copy(f, rc)
		f.Close()
		rc.Close()
		if copyErr != nil {
			return fmt.Errorf("write %s: %w", localPath, copyErr)
		}

		// Sync mtime
		os.Chtimes(localPath, entry.ModTime, entry.ModTime)
	}

	return nil
}

// SetHook sets the sync event hook for progress reporting.
// SetHook 设置同步事件钩子用于进度报告。
func (s *Syncer) SetHook(h transport.SyncHook) {
	if s.engine != nil {
		s.engine.SetHook(h)
	}
}

// Close releases all resources (SSH + SFTP connections).
// Close 释放所有资源（SSH + SFTP 连接）。
func (s *Syncer) Close() {
	if s.tr != nil {
		s.tr.Close()
		s.connected = false
	}
}

// GetFile downloads a remote file and returns a reader.
// GetFile 下载远端文件并返回 reader。
func (s *Syncer) GetFile(remote string) (io.ReadCloser, error) {
	if !s.connected {
		if err := s.Connect(); err != nil {
			return nil, err
		}
	}
	return s.tr.GetFile(remote)
}

// IsConnected returns whether the syncer has an active connection.
// IsConnected 返回是否有活跃连接。
func (s *Syncer) IsConnected() bool {
	return s.connected && s.tr != nil && s.tr.IsConnected()
}
