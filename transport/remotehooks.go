package transport

import (
	"fmt"
	"strings"
)

// HookResult holds the result of a single hook command execution.
// HookResult 保存单个钩子命令的执行结果。
type HookResult struct {
	Command string // command executed / 执行的命令
	Output  string // combined stdout+stderr / 输出
	Err     error  // error if failed / 错误（如有）
}

// ExecHooks runs a list of commands on the remote server sequentially.
// Returns results for each command. Stops early if a command fails and stopOnError is true.
// ExecHooks 在远端服务器顺序执行命令列表。
// 返回每个命令的结果。stopOnError 为 true 时命令失败即停止。
func (e *SyncEngine) ExecHooks(commands []string, stopOnError bool) []HookResult {
	var results []HookResult
	for _, cmd := range commands {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		out, err := e.transport.ExecOutput(cmd)
		results = append(results, HookResult{
			Command: cmd,
			Output:  out,
			Err:     err,
		})
		if err != nil && stopOnError {
			break
		}
	}
	return results
}

// HookFailed returns true if any hook result contains an error.
func HookFailed(results []HookResult) bool {
	for _, r := range results {
		if r.Err != nil {
			return true
		}
	}
	return false
}

// FormatHookResults formats hook results for display.
func FormatHookResults(results []HookResult) string {
	var sb strings.Builder
	for _, r := range results {
		status := "OK"
		if r.Err != nil {
			status = "FAIL"
		}
		sb.WriteString(fmt.Sprintf("    [%s] %s\n", status, r.Command))
		if r.Output != "" {
			for _, line := range strings.Split(r.Output, "\n") {
				if line != "" {
					sb.WriteString(fmt.Sprintf("           %s\n", line))
				}
			}
		}
		if r.Err != nil {
			sb.WriteString(fmt.Sprintf("           error: %v\n", r.Err))
		}
	}
	return sb.String()
}
