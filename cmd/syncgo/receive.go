// receive.go — remote receiver command
// Runs on the server side: read local file → generate signature → receive instructions → rebuild file.
// Communicates with the sender via stdin/stdout.
//
// receive.go — 远程 receiver 命令
// 运行在服务器端：读取本地文件 → 生成签名 → 接收指令 → 重建文件。
// 通过 stdin/stdout 与发送端通信。
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/winezer0/syncgo/syncer/agent"
)

func init() {
	receiveCmd := &cobra.Command{
		Use:    "receive <file path> / receive <文件路径>",
		Short:  "Receiver mode (internal, called by remote SSH) / 接收端模式（内部使用，由远程 SSH 调用）",
		Hidden: true,
		Run:    runReceive,
		Args:   cobra.ExactArgs(1),
	}
	receiveCmd.Flags().String("algo", "md5", "strong checksum algorithm / 强校验和算法")
	receiveCmd.Flags().Bool("no-cache", false, "skip signature cache (for checksum mode) / 跳过签名缓存（校验和模式用）")
	rootCmd.AddCommand(receiveCmd)
}

func runReceive(cmd *cobra.Command, args []string) {
	filePath := args[0]
	algo, _ := cmd.Flags().GetString("algo")
	noCache, _ := cmd.Flags().GetBool("no-cache")

	if err := agent.RunReceive(agent.ReceiveOptions{
		FilePath: filePath,
		Algo:     algo,
		NoCache:  noCache,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "RECEIVER ERROR: %v\n", err)
		os.Exit(1)
	}
}
