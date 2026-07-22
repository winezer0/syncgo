//go:build tui

// tui_enabled.go — TUI command implementation (compiled with -tags tui).
// tui_enabled.go — TUI 命令实现（使用 -tags tui 编译）。
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/winezer0/syncgo/config"
	"github.com/winezer0/syncgo/tui"
)

// registerTUICmd adds the "tui" subcommand to root.
func registerTUICmd(root *cobra.Command) {
	root.AddCommand(&cobra.Command{
		Use:   "tui",
		Short: "Open the terminal UI",
		Long: `Launch the interactive terminal user interface.

The TUI provides panes for dashboard (sync status overview),
mapping management (add/edit/delete sync tasks), server management
(test connection, deploy agent), a file explorer, and settings
(language, checksum algorithm, worker count).`,
		Run: runTUI,
	})
}

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
