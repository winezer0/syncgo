// embed_agent.go — Embeds the agent sub-module source tree for cross-compilation.
// This enables agent compilation even when syncgo is used as a library (go get),
// without requiring the original project source tree or network access.
//
// embed_agent.go — 嵌入 agent 子模块源码树，用于交叉编译。
// 即使 syncgo 作为库使用（go get），也能编译 agent，
// 无需原始项目源码树或网络访问。
package syncer

import "embed"

// agentSource contains the embedded agent sub-module source code.
// The agent/ directory is a standalone Go module with minimal dependencies
// (only go-rsync), enabling self-contained cross-compilation.
//
// agentSource 包含嵌入的 agent 子模块源码。
// agent/ 是独立的 Go 模块，仅依赖 go-rsync，
// 支持自包含的交叉编译。
//
//go:embed all:agent
var agentSource embed.FS
