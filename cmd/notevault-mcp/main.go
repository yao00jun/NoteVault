// Command notevault-mcp 是 NoteVault 的 MCP (Model Context Protocol) 服务端，
// 通过 stdio 与 Claude Code / Codex 等 AI Agent 通信，让外部 Agent 能读取
// （默认）乃至创建（--enable-write）用户的本地笔记。
//
// 启动：
//
//	notevault-mcp --workspace /path/to/vault
//	notevault-mcp --workspace /path/to/vault --enable-write
//
// 也可通过环境变量 NOTEVAULT_WORKSPACE 提供工作区路径。
//
// 协议：stdin 逐行读取 JSON-RPC 2.0 报文，stdout 逐行写出响应；
// 所有诊断日志只写 stderr，绝不污染 MCP 的 stdout 流。
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/notevault/notevault/internal/mcp"
)

func main() {
	workspace := flag.String("workspace", "", "NoteVault workspace (vault) directory")
	enableWrite := flag.Bool("enable-write", false, "Allow write tools (create_note). Off by default.")
	flag.Parse()

	ws := *workspace
	if ws == "" {
		ws = os.Getenv("NOTEVAULT_WORKSPACE")
	}
	if ws == "" {
		fmt.Fprintln(os.Stderr, "notevault-mcp: --workspace (or NOTEVAULT_WORKSPACE) is required")
		os.Exit(2)
	}
	info, err := os.Stat(ws)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "notevault-mcp: workspace %q is not a directory\n", ws)
		os.Exit(2)
	}

	server := mcp.NewServer(ws, *enableWrite)
	fmt.Fprintf(os.Stderr, "notevault-mcp: serving workspace %q (write=%v)\n", ws, *enableWrite)

	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if len(trimmed) > 0 {
				resp, hasResp := server.Process([]byte(trimmed))
				if hasResp {
					writer.Write(resp)
					writer.WriteByte('\n')
					writer.Flush()
				}
			}
		}
		if err != nil {
			// EOF 或读取错误：优雅退出（错误已非致命，无需特别处理）。
			break
		}
	}
}
