// exec.go — Execute remote SSH commands on configured servers.
// exec.go — 在已配置的服务器上执行远端 SSH 命令。
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/winezer0/syncgo/config"
	"github.com/winezer0/syncgo/transport"

	"github.com/spf13/cobra"
)

func init() {
	execCmd := &cobra.Command{
		Use:   "exec <server> <command>",
		Short: "Execute a command on a remote server via SSH",
		Long: `Run an arbitrary shell command on a configured remote server.

This is a standalone utility — no sync task is needed, only a server
defined in syncd.yaml. Useful for quick administration, debugging,
or one-off operations.

Examples:
  shuttle exec vps "ls -la /var/www"
  shuttle exec vps "systemctl restart nginx"
  shuttle exec vps "df -h && free -m"
  shuttle exec --all "uptime"
  shuttle exec vps --file deploy.sh

Flags:
  --all    Execute on ALL configured servers
  --file   Read command(s) from a local file instead of argument`,
		Args: cobra.RangeArgs(1, 2),
		Run:  runExec,
	}
	execCmd.Flags().StringVarP(&cfgPath, "config", "c", "syncd.yaml", "path to YAML config file")
	execCmd.Flags().Bool("all", false, "execute on all configured servers")
	execCmd.Flags().String("file", "", "read command from a local file")
	rootCmd.AddCommand(execCmd)
}

func runExec(cmd *cobra.Command, args []string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	allFlag, _ := cmd.Flags().GetBool("all")
	fileFlag, _ := cmd.Flags().GetString("file")

	// Determine the command to execute
	var command string
	if fileFlag != "" {
		data, err := os.ReadFile(fileFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read command file: %v\n", err)
			os.Exit(1)
		}
		command = strings.TrimSpace(string(data))
	} else if len(args) >= 2 {
		command = strings.Join(args[1:], " ")
	} else if !allFlag {
		fmt.Fprintf(os.Stderr, "Usage: shuttle exec <server> <command>\n")
		fmt.Fprintf(os.Stderr, "       shuttle exec --all <command>\n")
		os.Exit(1)
	} else {
		// --all mode: first arg is the command
		command = args[0]
	}

	if command == "" {
		fmt.Fprintf(os.Stderr, "Error: empty command\n")
		os.Exit(1)
	}

	// Determine target servers
	var servers []config.Server
	if allFlag {
		servers = cfg.Servers
		if len(servers) == 0 {
			fmt.Fprintf(os.Stderr, "No servers configured in %s\n", cfgPath)
			os.Exit(1)
		}
	} else {
		serverName := args[0]
		srv := cfg.GetServer(serverName)
		if srv == nil {
			fmt.Fprintf(os.Stderr, "Server '%s' not found in config.\n", serverName)
			fmt.Fprintf(os.Stderr, "Available servers:\n")
			for _, s := range cfg.Servers {
				fmt.Fprintf(os.Stderr, "  - %s (%s@%s:%d)\n", s.Name, s.User, s.Host, s.Port)
			}
			os.Exit(1)
		}
		servers = []config.Server{*srv}
	}

	// Execute on each server
	exitCode := 0
	for _, srv := range servers {
		fmt.Printf("[%s] %s@%s:%d\n", srv.Name, srv.User, srv.Host, srv.Port)

		tr := transport.NewSFTP(transport.SFTPConfig{
			Host:    srv.Host,
			Port:    srv.Port,
			User:    srv.User,
			KeyFile: srv.KeyFile,
			Pass:    srv.Pass,
		})

		if err := tr.Connect(); err != nil {
			fmt.Fprintf(os.Stderr, "  Connect failed: %v\n", err)
			exitCode = 1
			continue
		}

		out, err := tr.ExecOutput(command)
		tr.Close()

		if out != "" {
			fmt.Println(out)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Exit error: %v\n", err)
			exitCode = 1
		}

		// Separator between multiple servers
		if len(servers) > 1 {
			fmt.Println()
		}
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
