package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/winezer0/syncgo/config"
	"github.com/winezer0/syncgo/transport"
	"github.com/winezer0/syncgo/util"

	delta "github.com/henryborner/go-rsync"
)

// highRiskDryExts are high-risk file extensions for extra warnings during dry-run.
// highRiskDryExts 高危文件扩展名，dry-run 时额外警告。
var highRiskDryExts = map[string]string{
	".db": "database", ".sql": "database", ".sqlite": "database", ".sqlite3": "database",
	".mdb": "database", ".myd": "database", ".myi": "database", ".frm": "database", ".ibd": "database",
	".key": "private key", ".pem": "certificate/key", ".crt": "certificate",
	".p12": "keystore", ".pfx": "keystore", ".jks": "keystore",
	".conf": "config", ".cfg": "config", ".ini": "config",
	".yaml": "config", ".yml": "config", ".env": "config",
	".service": "systemd unit", ".timer": "systemd unit",
}

// dryRunHook lists each file's operation in dry-run mode.
// dryRunHook 在 dry-run 模式下列出每个文件的操作。
type dryRunHook struct {
	dry          bool
	deletedFiles []string
}

func (h *dryRunHook) OnSyncStart(name string, total int) error {
	fmt.Printf("  %d files to check...\n", total)
	return nil
}
func (h *dryRunHook) OnFileStart(path string, size int64) error     { return nil }
func (h *dryRunHook) OnFileProgress(path string, sent, total int64) {}
func (h *dryRunHook) OnFileDone(evt transport.FileEvent) error {
	switch {
	case evt.IsNew:
		fmt.Printf("  %s  %s\n", util.Pad("NEW", 5), evt.RelPath)
	case evt.IsUpdated:
		tag := "UPD"
		if evt.IsDelta {
			tag = "Δ"
		}
		fmt.Printf("  %s  %s  (%s)\n", util.Pad(tag, 5), evt.RelPath, util.FormatBytes(evt.FileSize))
	case evt.IsDeleted:
		h.deletedFiles = append(h.deletedFiles, evt.RelPath)
		fmt.Printf("  %s  %s\n", util.Pad("DEL", 5), evt.RelPath)
	case evt.IsProtected:
		fmt.Printf("  %s  %s\n", util.Pad("PROT", 5), evt.RelPath)
	default:
		fmt.Printf("  %s  %s\n", util.Pad("SKIP", 5), evt.RelPath)
	}
	return nil
}
func (h *dryRunHook) OnSyncDone(stats *transport.SyncStats) error {
	// secondary warning: high-risk files in dry-run delete list
	// 二次警告：dry-run 删除清单中有高危文件
	var risky []string
	for _, f := range h.deletedFiles {
		ext := strings.ToLower(filepath.Ext(f))
		base := strings.ToLower(filepath.Base(f))
		if kind, ok := highRiskDryExts[ext]; ok {
			risky = append(risky, fmt.Sprintf("  [!] %s (%s)", f, kind))
		} else if ext == "" {
			// no-extension files may also be important
			// 无扩展名文件也可能是重要的
			if kind, ok := highRiskDryExts["."+base]; ok {
				risky = append(risky, fmt.Sprintf("  [!] %s (%s)", f, kind))
			}
		}
	}
	if len(risky) > 0 {
		fmt.Println()
		fmt.Println("  !! WARNING: High-risk files in delete list:")
		for _, r := range risky {
			fmt.Println(r)
		}
		fmt.Println("  Review carefully before running without --dry-run!")
	}
	return nil
}

// doSync 执行同步任务
func doSync(taskName, cfgPath string, dryRun, verbose bool, workers int, algoName string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid config: %v\n", err)
		os.Exit(1)
	}

	// 应用配置中的哈希算法
	if algoName != "" {
		delta.SetDefault(algoName)
	} else if cfg.Checksum != "" {
		delta.SetDefault(cfg.Checksum)
	}

	// workers override
	if workers <= 0 {
		workers = cfg.Workers
		if workers <= 0 {
			workers = 4
		}
	}

	var tasks []config.Task
	if taskName != "" {
		t := cfg.GetTask(taskName)
		if t == nil {
			fmt.Fprintf(os.Stderr, "Task not found: %s\n", taskName)
			os.Exit(1)
		}
		tasks = append(tasks, *t)
	} else {
		tasks = cfg.Tasks
	}

	if dryRun {
		fmt.Println("Dry run — no changes will be made")
		fmt.Println()
	}

	for _, task := range tasks {
		fmt.Printf("Task: %s\n  Source: %s\n  Target: %s\n", task.Name, task.Source, task.Target)

		serverName, remotePath := config.ParseTarget(task.Target)
		if serverName == "" {
			fmt.Println("  Local sync not yet supported")
			continue
		}

		server := cfg.GetServer(serverName)
		if server == nil {
			fmt.Fprintf(os.Stderr, "  Server not found: %s\n", serverName)
			continue
		}

		// 连接
		fmt.Printf("  Connecting %s@%s:%d...\n", server.User, server.Host, server.Port)
		sftp := transport.NewSFTP(transport.SFTPConfig{
			Host: server.Host, Port: server.Port,
			User: server.User, AuthType: string(server.AuthType),
			KeyFile: server.KeyFile, Pass: server.Pass,
		})

		if err := sftp.Connect(); err != nil {
			fmt.Fprintf(os.Stderr, "  Connect failed: %v\n", err)
			continue
		}

		// 同步
		engine := transport.NewSyncEngine(sftp)
		engine.SetHook(&dryRunHook{dry: dryRun})

		// Execute before hooks
		stopOnError := task.Hooks.OnError == "" || task.Hooks.OnError == config.HookAbort
		if len(task.Hooks.Before) > 0 && !dryRun {
			fmt.Printf("  Hooks [before]:\n")
			results := engine.ExecHooks(task.Hooks.Before, stopOnError)
			fmt.Print(transport.FormatHookResults(results))
			if transport.HookFailed(results) {
				switch task.Hooks.OnError {
				case config.HookAbort, "":
					fmt.Fprintf(os.Stderr, "  Before hook failed — aborting task.\n")
					sftp.Close()
					continue
				case config.HookWarn:
					fmt.Fprintf(os.Stderr, "  Before hook failed (warn mode) — continuing.\n")
				}
			}
		}

		// Determine sync mode
		syncMode := task.Options.SyncMode
		if syncMode == "" {
			syncMode = config.SyncOverlayPatch
		}

		var stats *transport.SyncStats
		var syncErr error

		switch syncMode {
		case config.SyncFullReplace:
			fmt.Printf("  Mode: full_replace (tar.gz pack → upload → extract)\n")
			stats, syncErr = engine.SyncFullReplace(transport.SyncOptions{
				Source:   task.Source,
				Target:   remotePath,
				Exclude:  task.Options.Exclude,
				Protect:  server.Protect,
				DryRun:   dryRun,
				SkipDots: !task.Options.ShowDots,
				Workers:  workers,
				Flat:     task.Options.Flat,
			})
		default:
			// Overlay mode with incremental toggle
			incremental := task.Options.IsIncremental(cfg.IsIncremental())
			opts := transport.SyncOptions{
				Source:   task.Source,
				Target:   remotePath,
				Delete:   task.Options.Delete,
				Exclude:  task.Options.Exclude,
				Protect:  server.Protect,
				Checksum: task.Options.Checksum,
				DryRun:   dryRun,
				SkipDots: !task.Options.ShowDots,
				Workers:  workers,
				Flat:     task.Options.Flat,
			}
			// When incremental is disabled, force full comparison + delete
			if !incremental {
				opts.Delete = true
				opts.Checksum = true
				fmt.Printf("  Mode: overlay (incremental OFF — full compare + delete)\n")
			}
			stats, syncErr = engine.Sync(opts)
		}

		sftp.Close()

		// Execute after hooks (reconnect for hooks if sync succeeded)
		if len(task.Hooks.After) > 0 && !dryRun && syncErr == nil {
			hookTr := transport.NewSFTP(transport.SFTPConfig{
				Host: server.Host, Port: server.Port,
				User: server.User, AuthType: string(server.AuthType),
				KeyFile: server.KeyFile, Pass: server.Pass,
			})
			if err := hookTr.Connect(); err == nil {
				hookEngine := transport.NewSyncEngine(hookTr)
				fmt.Printf("  Hooks [after]:\n")
				results := hookEngine.ExecHooks(task.Hooks.After, stopOnError)
				fmt.Print(transport.FormatHookResults(results))
				if transport.HookFailed(results) && (task.Hooks.OnError == config.HookAbort || task.Hooks.OnError == "") {
					fmt.Fprintf(os.Stderr, "  After hook failed.\n")
				}
				hookTr.Close()
			}
		}

		if syncErr != nil {
			fmt.Fprintf(os.Stderr, "  Sync failed: %v\n", syncErr)
			continue
		}

		prefix := ""
		if dryRun {
			prefix = "[DRY RUN] "
		}
		fmt.Printf("  %sDone | files:%d new:%d updated:%d",
			prefix, stats.TotalFiles, stats.NewFiles, stats.UpdatedFiles)
		if stats.SkippedFiles > 0 {
			fmt.Printf(" skipped:%d", stats.SkippedFiles)
		}
		if stats.ProtectedFiles > 0 {
			fmt.Printf(" protected:%d", stats.ProtectedFiles)
		}
		if stats.DeletedFiles > 0 {
			fmt.Printf(" deleted:%d", stats.DeletedFiles)
		}
		if stats.ConflictedFiles > 0 {
			fmt.Printf(" conflicts:%d", stats.ConflictedFiles)
		}
		if stats.DeltaFiles > 0 {
			fmt.Printf(" delta:%d", stats.DeltaFiles)
		}
		fmt.Printf(" | %s total", util.FormatBytes(stats.TotalBytes))
		if stats.SentBytes > 0 {
			savedPct := float64(0)
			if stats.TotalBytes > 0 {
				savedPct = float64(stats.TotalBytes-stats.SentBytes) / float64(stats.TotalBytes) * 100
			}
			fmt.Printf("  sent:%s (%.0f%% saved)", util.FormatBytes(stats.SentBytes), savedPct)
		}
		if verbose {
			if stats.DeltaSaved > 0 {
				fmt.Printf("  delta-matched:%s", util.FormatBytes(stats.DeltaSaved))
			}
		}
		if len(stats.Errors) > 0 {
			fmt.Printf(" | errors:%d", len(stats.Errors))
			if verbose {
				for _, e := range stats.Errors {
					fmt.Printf("\n    - %v", e)
				}
			}
		}
		fmt.Println()
	}

	if dryRun {
		fmt.Println("Dry run complete — use 'shuttle push' to sync")
	}
}
