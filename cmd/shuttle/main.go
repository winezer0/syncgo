// main.go — Shuttle CLI entry point (Cobra)
// main.go — Shuttle CLI entry point (Cobra)
// main.go — Shuttle CLI 入口 (Cobra)
package main

import (
	"fmt"
	"os"
	"runtime"

	"strings"

	"github.com/winezer0/syncgo/config"
	"github.com/winezer0/syncgo/tui"

	delta "github.com/henryborner/go-rsync"
	"github.com/spf13/cobra"
)

var (
	cfgPath    string
	dryRun     bool
	verbose    bool
	workers    int
	algoName   string
	schemaFlag bool

	versionStr = "0.1.5.9"
	rootCmd    = &cobra.Command{
		Use:   "shuttle",
		Short: "Incremental file sync over SSH",
		Long: `Shuttle syncs local directories to remote Linux servers over SSH.

It compares source and target using the rsync delta algorithm:
files that exist on both sides transfer only a checksum signature
(a few KB) instead of the full file content. Only changed portions
of files are sent across the network.

Mappings between local paths and remote servers are defined in a
syncd.yaml config file. A terminal UI (TUI) is also available for
interactive management.

Getting started:
  shuttle init                 create a config template
  shuttle config --schema      full field reference with examples
  shuttle push                 run all sync tasks
  shuttle exec <server> "cmd"  run a remote SSH command`,
		Version: versionStr,
	}
)

func main() {
	// Double-click launch: start TUI directly (no terminal needed).
	// 双击启动：直接进入 TUI 界面。
	if len(os.Args) == 1 {
		runTUI(nil, nil)
		return
	}

	// push
	pushCmd := &cobra.Command{
		Use:   "push [task name]",
		Short: "Execute one or all sync tasks",
		Long: `Run sync tasks defined in syncd.yaml.

If a task name is given, only that task runs. Otherwise all tasks
are executed in order. Each task connects to its target server via
SSH, compares local and remote files, and transfers only the
differences (delta).

Quick reference:
  Shuttle detects folder vs file from the filesystem, not from trailing
  slashes (unlike rsync).  Trailing \ or / is purely a visual convention.
  See 'shuttle config --schema' for the complete field reference.`,
		Run: runPush,
	}
	pushCmd.Flags().StringVarP(&cfgPath, "config", "c", "syncd.yaml", "path to YAML config file")
	pushCmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be transferred without making changes")
	pushCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print per-file transfer details and wire bytes sent")
	pushCmd.Flags().IntVarP(&workers, "workers", "w", 0, "parallel delta workers (0 uses config default, 1=serial, max 8)")
	pushCmd.Flags().StringVar(&algoName, "algo", "", "checksum algorithm: md5, sha256, xxh64, or xxh3 (overrides config)")
	rootCmd.AddCommand(pushCmd)

	// tui
	rootCmd.AddCommand(&cobra.Command{
		Use:   "tui",
		Short: "Open the terminal UI",
		Long: `Launch the interactive terminal user interface.

The TUI provides panes for dashboard (sync status overview),
mapping management (add/edit/delete sync tasks), server management
(test connection, deploy agent), a file explorer, and settings
(language, checksum algorithm, worker count).`,
		Run: runTUI,
	})

	// list
	rootCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Print all tasks and servers from syncd.yaml",
		Long:  `Read syncd.yaml and print every configured task and server to stdout.`,
		Run:   runList,
	})

	// config
	configCmd := &cobra.Command{
		Use:   "config [--schema]",
		Short: "Print the syncd.yaml config summary or field reference",
		Long: `Load syncd.yaml and display a structured summary:
servers (name, host, port, user, auth method) and tasks
(name, source, target, enabled options).

With --schema: print a complete field reference including type
info, descriptions, examples (folder + single file), and the
list of available checksum algorithms.`,
		Run: runConfig,
	}
	configCmd.Flags().BoolVar(&schemaFlag, "schema", false, "print full config field reference with examples")
	rootCmd.AddCommand(configCmd)

	// test
	testCmd := &cobra.Command{
		Use:   "test <server name>",
		Short: "Verify SSH connectivity to a server",
		Long: `Open an SSH connection to the named server and report success or failure.

This is useful before running sync tasks to ensure the server
is reachable and the key or password is accepted.`,
		Args: cobra.ExactArgs(1),
		Run:  runTest,
	}
	rootCmd.AddCommand(testCmd)

	// init
	rootCmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Write a syncd.yaml template to the current directory",
		Long: `Create a new syncd.yaml file with commented examples for
both folder syncs (website deployment) and single-file syncs
(config push). Safe to run — will not overwrite an existing file.

Next steps:
  Edit syncd.yaml to set your server and task
  shuttle config --schema   view all available fields
  shuttle test <server>     verify SSH connectivity
  shuttle push --dry-run    preview what will be transferred`,
		Run: runInit,
	})

	// version
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version, Go runtime, and available checksum algorithms",
		Long:  `Display the Shuttle version, Go compiler version, target OS/arch, and the list of supported strong checksum algorithms.`,
		Run:   runVersion,
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runVersion(cmd *cobra.Command, args []string) {
	fmt.Printf("Shuttle v%s\n", versionStr)
	fmt.Printf("  Go:     %s\n", runtime.Version())
	fmt.Printf("  OS:     %s\n", runtime.GOOS)
	fmt.Printf("  Arch:   %s\n", runtime.GOARCH)
	fmt.Printf("  Strong: %s\n", delta.GetDefault())
	fmt.Printf("  Algos:  %s\n", strings.Join(delta.ListAlgos(), ", "))
}

func runPush(cmd *cobra.Command, args []string) {
	taskName := ""
	if len(args) > 0 {
		taskName = args[0]
	}
	doSync(taskName, cfgPath, dryRun, verbose, workers, algoName)
}

func runConfig(cmd *cobra.Command, args []string) {
	if schemaFlag {
		runSchema()
		return
	}
	cfg, err := config.Load("syncd.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "No config found: %v\n", err)
		fmt.Println("Run 'shuttle init' to create one.")
		return
	}
	fmt.Printf("Config: syncd.yaml  (version %s)\n", cfg.Version)
	fmt.Printf("Language: %s  |  Checksum: %s  |  Workers: %d\n",
		cfg.Language, cfg.Checksum, cfg.Workers)
	fmt.Printf("Servers: %d  |  Tasks: %d\n", len(cfg.Servers), len(cfg.Tasks))
	fmt.Println()
	fmt.Println("── Servers ──")
	for _, s := range cfg.Servers {
		auth := "key"
		if s.Pass != "" {
			auth = "password"
		}
		fmt.Printf("  %-15s %s@%s:%d  (%s)\n", s.Name, s.User, s.Host, s.Port, auth)
	}
	fmt.Println()
	fmt.Println("── Tasks ──")
	for _, t := range cfg.Tasks {
		flags := ""
		if t.Options.Delete {
			flags += " delete"
		}
		if t.Options.Checksum {
			flags += " checksum"
		}
		if t.Options.Flat {
			flags += " flat"
		}
		if flags == "" {
			flags = " (defaults)"
		}
		fmt.Printf("  %-15s %s → %s%s\n", t.Name, t.Source, t.Target, flags)
	}
}

func runList(cmd *cobra.Command, args []string) {
	cfg, err := config.Load("syncd.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "No config: %v\n", err)
		return
	}
	fmt.Println("Tasks:")
	for _, t := range cfg.Tasks {
		fmt.Printf("  %-15s %s\n", t.Name, t.Source)
	}
	fmt.Println()
	fmt.Println("Servers:")
	for _, s := range cfg.Servers {
		fmt.Printf("  %-15s %s@%s:%d\n", s.Name, s.User, s.Host, s.Port)
	}
}

func runTest(cmd *cobra.Command, args []string) {
	serverName := args[0]
	cfg, err := config.Load("syncd.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "No config: %v\n", err)
		os.Exit(1)
	}
	s := cfg.GetServer(serverName)
	if s == nil {
		fmt.Fprintf(os.Stderr, "Server not found: %s\n", serverName)
		os.Exit(1)
	}
	fmt.Printf("Testing %s@%s:%d ...\n", s.User, s.Host, s.Port)
	if err := testDial(s.Host, s.Port, s.User, s.KeyFile, s.Pass); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OK — connected successfully")
}

func runSchema() {
	fmt.Println(`syncd.yaml Configuration Reference
=====================================

Top-Level Fields
────────────────────────
  version      string    Config version, currently "1.0"
  language     string    UI language: en / zh (default zh)
  checksum     string    Default strong checksum: md5 / sha256 / xxh64 / xxh3 (default xxh64)
  workers      int       Parallel delta workers: 1=serial, 2/4/8=parallel (default 4)
  incremental  bool      Global incremental toggle (default true)
                         true: only transfer changed files (mtime/size or checksum)
                         false: force full comparison + delete orphans
  retry        Retry     Retry policy for transient failures
  servers      []Server  Server connection list
  tasks        []Task    Sync task list

Retry
────────────────────────
  max_retries  int       Max retry attempts (default 3)
  delay_ms     int       Delay between retries in milliseconds (default 1000)

Server
────────────────────────
  name       string    Server name, referenced in task.target
  host       string    SSH host address (IP or domain)
  port       int       SSH port (default 22)
  user       string    Login username
  auth_type  string    Authentication type: auto / password / private_key (default auto)
                       auto: try private key first, then password
                       password: password only
                       private_key: private key file only
  key_file   string    SSH private key path, e.g. ~/.ssh/id_ed25519 (preferred over password)
  password   string    Login password (fallback when key is unavailable; plaintext not recommended)
  protect    []string  Protect patterns (glob) — matching remote files are NEVER overwritten or deleted
                       Example: ["*.db", "*.pem", "config.yaml", "secrets/"]

Task
────────────────────────
  name       string    Task name
  source     string    Local source path.
                       Shuttle detects folder vs file from the filesystem (os.Stat),
                       not from a trailing slash.  The trailing \ or / is a visual
                       convention — it has no effect on behavior.
                       ── Folder ──
                         Source that exists as a directory on disk.
                         Contents are mapped into target (respecting flat).
                         Examples:
                           E:\projects\dist\
                           /home/deploy/site/
                       ── Single file ──
                         Source that exists as a regular file on disk.
                         Synced directly to the target path.
                         Examples:
                           E:\configs\nginx.conf
                           /etc/myapp/config.yaml
                       ⚠ Unlike rsync, shuttle ignores trailing slashes.
                       E:\dist\ and E:\dist behave identically.
  target     string    Remote target, format: <server name>:<path>
                       Shuttle joins target + relative path.  Trailing / has no
                       special meaning — filepath.Join handles both forms the same.
                       ── Folder sync ──
                         Example: myserver:/var/www/html/
                           source=E:\dist\  →  files go to /var/www/html/*
                       ── Single file sync ──
                         Example: myserver:/etc/nginx/nginx.conf
                           source=E:\configs\nginx.conf  →  overwrites /etc/nginx/nginx.conf
  options    Options   Sync options

Options
────────────────────────
  sync_mode    string    Sync strategy: overlay (default) / full_replace
                         overlay: incremental upload, preserve remote-only files
                         full_replace: tar.gz pack → upload → extract → clean orphans
  delete     bool      Delete extra files on the remote side (default false)
                       When enabled, remote files not present locally will be removed.
                       ⚠ Only applies to overlay mode folder syncs.
  exclude    []string  Glob patterns to skip — matching files/dirs are not transferred
                       Example: ["*.tmp", ".git/", "node_modules/"]
  checksum   bool      Use strong checksums to detect file changes (default false)
                       false: compare by mtime + file size (fast, 1-second precision)
                       true:  compare by full strong checksum (accurate, slower)
  flat       bool      Flat mapping (default false, only meaningful for folder syncs)
                       false: source folder name appears in the target path.
                              E:\projects\dist\  →  /var/www/html/dist/...
                       true:  map source contents directly, no outer folder.
                              E:\projects\dist\  →  /var/www/html/...
  show_dots  bool      Transfer hidden files/directories (default false)
                       Hidden files are those whose name starts with a dot (.)
  incremental  bool    Task-level incremental toggle (overrides global)
                       true: skip unchanged files (default behavior)
                       false: force full comparison + delete orphans

Sync Modes Explained
────────────────────────
  overlay (default):
    - Upload new/modified files only
    - Preserve remote-only files (configs, keys, logs)
    - Optional delete of orphans (delete: true)
    - Use case: iterative deployment, preserve remote configs

  full_replace:
    - Pack local directory into tar.gz
    - Upload archive to remote /tmp
    - Clean target directory + extract archive
    - Remote-only files are DELETED (strong consistency)
    - Use case: clean release, binary update, environment reset
    - Requires: remote server must have 'tar' command

Hooks (Remote Commands)
────────────────────────
  hooks.before   []string  Commands to run on remote BEFORE sync starts
                           Example: ["systemctl stop nginx", "cp -r /var/www /tmp/backup"]
  hooks.after    []string  Commands to run on remote AFTER sync completes
                           Example: ["systemctl start nginx", "rm -rf /tmp/cache/*"]
  hooks.on_error string    Behavior when a hook fails: abort (default) / warn / ignore
                           abort: stop the sync task immediately
                           warn:  print warning but continue sync
                           ignore: silently continue

  Notes:
    - Hooks run via SSH exec on the same server as the task target
    - Commands run sequentially in listed order
    - before hooks run after SSH connect, before any file transfer
    - after hooks run only if sync succeeded (no sync errors)
    - Use full paths for binaries (e.g. /usr/sbin/nginx) since
      SSH exec may not load your full shell PATH

Strong Checksum Algorithms
────────────────────────
  xxh64      64-bit xxHash (default), fastest — good for LAN/SSD
  xxh3       128-bit xxH3, fast non-crypto hash with wider output (~2⁻⁶⁴ collision)
  md5        128-bit MD5, best cross-platform compatibility
  sha256     256-bit SHA-2, strongest — use when integrity matters most
  (All algorithms have SIMD-accelerated assembly paths on amd64.)

Examples
────────────────────────
  # Folder sync: deploy a website build (incremental overlay)
  tasks:
    - name: web
      source: E:\projects\dist\
      target: myserver:/var/www/html/
      options:
        sync_mode: overlay
        delete: true
        exclude: [".DS_Store", "*.map"]

  # Full replace: clean binary release
  tasks:
    - name: app-release
      source: E:\builds\app\
      target: myserver:/opt/app/
      options:
        sync_mode: full_replace
        exclude: ["*.log"]

  # Single file sync: push a config file
  tasks:
    - name: nginx-config
      source: E:\configs\nginx.conf
      target: myserver:/etc/nginx/nginx.conf
      options:
        checksum: true

Usage
────────────────────────
  View current config:    shuttle config
  Show this reference:    shuttle config --schema
  Generate a template:    shuttle init
  Run sync tasks:         shuttle push [task] [--dry-run]
  Test SSH connection:    shuttle test <server>
  Deploy delta agent:     shuttle deploy-agent <server>
  Run remote command:     shuttle exec <server> "command"
  Run on all servers:     shuttle exec --all "command"
  Command from file:      shuttle exec <server> --file script.sh`)
}

func runInit(cmd *cobra.Command, args []string) {
	if _, err := os.Stat("syncd.yaml"); err == nil {
		fmt.Println("syncd.yaml already exists")
		return
	}
	os.WriteFile("syncd.yaml", []byte(initTemplate), 0644)
	fmt.Println("Created syncd.yaml — edit it and run 'shuttle push'")
	fmt.Println("Run 'shuttle config --schema' for a full field reference.")
}

const initTemplate = `# Shuttle 同步配置文件
# 用法: shuttle push [任务名]
# 完整参考: shuttle config --schema

version: "1.0"
language: zh               # en / zh
checksum: xxh64            # md5 / sha256 / xxh64 / xxh3
workers: 4                 # delta 并行数: 1=串行 2/4/8=并行
incremental: true          # 全局增量开关: true=仅传变更文件  false=全量比对+删除

retry:                     # 重试策略
  max_retries: 3           # 最大重试次数
  delay_ms: 1000           # 重试间隔(毫秒)

servers:
  - name: myserver
    host: 192.168.1.100
    port: 22
    user: deploy
    auth_type: auto        # auto / password / private_key
    key_file: ~/.ssh/id_ed25519
    protect:                # 保护列表：匹配的远端文件绝不覆盖/删除
      - "*.db"
      - "*.pem"
      - ".env"

tasks:
  # ── 示例1: 文件夹增量同步（部署网站）──
  - name: web
    source: E:\projects\website\dist\
    target: myserver:/var/www/html/
    options:
      sync_mode: overlay    # overlay=增量覆盖 / full_replace=压缩全量替换
      delete: true           # 删除远端多余文件（仅 overlay 模式有效）
      exclude:
        - "*.tmp"
        - ".DS_Store"
      checksum: false        # false: 比大小+时间  true: 比文件内容哈希
      flat: false            # true: 不套源文件夹名，直接映射内容
      incremental: true      # 任务级增量开关，覆盖全局设置

  # ── 示例2: 压缩全量替换（纯净发布）──
  # - name: release
  #   source: E:\builds\app\
  #   target: myserver:/opt/app/
  #   options:
  #     sync_mode: full_replace  # tar.gz打包→上传→解压→清理冗余
  #     exclude: ["*.log"]
  #   hooks:
  #     before:
  #       - "systemctl stop myapp"       # 同步前停服务
  #     after:
  #       - "systemctl start myapp"      # 同步后重启
  #       - "rm -rf /opt/app/cache/*"    # 清缓存
  #     on_error: abort                  # abort=中止 / warn=警告 / ignore=忽略

  # ── 示例3: 单文件同步（推送配置）──
  # - name: nginx-config
  #   source: E:\configs\nginx.conf
  #   target: myserver:/etc/nginx/nginx.conf
  #   options:
  #     checksum: true       # 单文件建议开启，精确判断是否需要更新
`

func runTUI(cmd *cobra.Command, args []string) {
	cfg, err := config.Load("syncd.yaml")
	if err != nil {
		if os.IsNotExist(err) {
			// First launch: generate default config then enter TUI
			os.WriteFile("syncd.yaml", []byte(initTemplate), 0644)
			fmt.Println("Created syncd.yaml — editing in TUI...")
			cfg, _ = config.Load("syncd.yaml")
		} else {
			fmt.Fprintf(os.Stderr, "Config load failed: %v\n", err)
			os.Exit(1)
		}
	}

	if err := tui.Run(cfg, "syncd.yaml"); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
