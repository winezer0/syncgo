// deploy_agent.go — Deploy syncgo agent to remote Linux server (CLI wrapper).
// The core logic is in syncer.DeployAgent; this file only handles CLI plumbing.
//
// deploy_agent.go — 部署 syncgo agent 到远端 Linux 服务器（CLI 包装）。
// 核心逻辑在 syncer.DeployAgent；本文件仅处理 CLI 交互。
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/winezer0/syncgo/config"
	"github.com/winezer0/syncgo/syncer"
)

func init() {
	deployCmd := &cobra.Command{
		Use:   "deploy-agent <server name>",
		Short: "Deploy syncgo agent to a remote Linux server",
		Long: `Deploy the syncgo agent binary to the remote server via SFTP.

The agent enables rsync-style delta transfers: only changed portions
of files are sent over the network instead of full file content.

Agent binary resolution order:
  1. Local file: syncgo_linux_<arch> in the program directory
  2. Download from GitHub Releases (v` + versionStr + `)
  3. Cross-compile from source (requires local Go toolchain)

Steps performed:
  1. Connect to the server and detect CPU architecture (uname -m)
  2. Resolve agent binary (local → release → build)
  3. Upload the binary to ~/.local/bin/syncgo on the server
  4. Set executable permissions (chmod +x)
  5. Verify by running 'syncgo version' on the remote

After deployment, 'syncgo push' will automatically use delta
transfers for updated files, significantly reducing bandwidth usage.`,
		Args: cobra.ExactArgs(1),
		Run:  runDeployAgent,
	}
	deployCmd.Flags().StringVarP(&cfgPath, "config", "c", "syncd.yaml", "path to YAML config file")
	rootCmd.AddCommand(deployCmd)
}

func runDeployAgent(cmd *cobra.Command, args []string) {
	serverName := args[0]

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	server := cfg.GetServer(serverName)
	if server == nil {
		fmt.Fprintf(os.Stderr, "Server not found: %s\n", serverName)
		os.Exit(1)
	}

	fmt.Printf("Connecting to %s@%s:%d ...\n", server.User, server.Host, server.Port)

	s := syncer.NewSyncerWithServer(*cfg, *server)
	defer s.Close()

	opts := syncer.DeployAgentOptions{
		Version: versionStr,
		Progress: func(msg string) {
			fmt.Println("  " + msg)
		},
	}

	ctx := context.Background()
	if err := s.DeployAgent(ctx, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Deploy failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n  Delta incremental transfers are now enabled for this server!")
}
