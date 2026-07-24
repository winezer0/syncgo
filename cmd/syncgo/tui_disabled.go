//go:build !tui

// tui_disabled.go — TUI stub when built without -tags tui.
// tui_disabled.go — 未使用 -tags tui 编译时的 TUI 占位实现。
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// registerTUICmd adds a stub "tui" subcommand that prints a hint.
func registerTUICmd(root *cobra.Command) {
	root.AddCommand(&cobra.Command{
		Use:   "tui",
		Short: "Open the terminal UI (not available in this build)",
		Run:   runTUI,
	})
}

func runTUI(cmd *cobra.Command, args []string) {
	fmt.Fprintln(os.Stderr, "TUI is not available in this build.")
	fmt.Fprintln(os.Stderr, "Rebuild with: go build -tags tui ./cmd/syncgo")
	os.Exit(1)
}
