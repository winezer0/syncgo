// syncgo-agent — minimal agent binary for delta receive.
// This is the only entry point deployed to remote servers.
// It reads flags from os.Args (no cobra dependency) and calls agent.RunReceive.
//
// syncgo-agent — 用于 delta 接收的最小 agent 二进制。
// 这是部署到远端服务器的唯一入口。
// 从 os.Args 读取参数（无 cobra 依赖），调用 agent.RunReceive。
package main

import (
	"fmt"
	"os"

	"github.com/winezer0/syncgo/syncer/agent"
)

func main() {
	// Minimal flag parsing without cobra:
	// syncgo-agent receive [--algo <algo>] [--no-cache] <file-path>
	//
	// Also supports being called as just:
	// syncgo-agent receive <file-path>
	// (the main syncgo binary calls: syncgo receive --algo xxh64 '/path/to/file')

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: syncgo-agent receive [--algo algo] [--no-cache] <file>")
		os.Exit(1)
	}

	// Handle "version" command (used by deploy verification)
	if os.Args[1] == "version" {
		fmt.Println("syncgo-agent")
		return
	}

	if os.Args[1] != "receive" {
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}

	algo := "md5"
	noCache := false
	var filePath string

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--algo":
			if i+1 < len(args) {
				i++
				algo = args[i]
			}
		case "--no-cache":
			noCache = true
		default:
			filePath = args[i]
		}
	}

	if filePath == "" {
		fmt.Fprintln(os.Stderr, "error: file path required")
		os.Exit(1)
	}

	if err := agent.RunReceive(agent.ReceiveOptions{
		FilePath: filePath,
		Algo:     algo,
		NoCache:  noCache,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "RECEIVER ERROR: %v\n", err)
		os.Exit(1)
	}
}
