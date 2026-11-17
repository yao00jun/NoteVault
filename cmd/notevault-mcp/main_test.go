package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type rpcReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeReplies(t *testing.T, stdout string) []rpcReply {
	t.Helper()
	var replies []rpcReply
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var reply rpcReply
		if err := json.Unmarshal([]byte(line), &reply); err != nil {
			t.Fatalf("stdout line is not JSON-RPC: %q (%v)", line, err)
		}
		replies = append(replies, reply)
	}
	return replies
}

func TestRunRequiresWorkspace(t *testing.T) {
	t.Setenv("NOTEVAULT_WORKSPACE", "")
	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay clean on argument errors, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--workspace") {
		t.Fatalf("stderr should explain the missing workspace, got %q", stderr.String())
	}
}

func TestRunRejectsNonDirectoryWorkspace(t *testing.T) {
	file := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(file, []byte("# x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--workspace", file}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "is not a directory") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunServesJSONRPCOnStdio(t *testing.T) {
	dir := t.TempDir()
	requests := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"

	var stdout, stderr bytes.Buffer
	code := run([]string{"--workspace", dir}, strings.NewReader(requests), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}

	replies := decodeReplies(t, stdout.String())
	// 通知不带 id，不应产生任何响应行。
	if len(replies) != 2 {
		t.Fatalf("got %d replies, want 2: %s", len(replies), stdout.String())
	}
	if string(replies[0].ID) != "1" || string(replies[1].ID) != "2" {
		t.Fatalf("unexpected reply ids: %s / %s", replies[0].ID, replies[1].ID)
	}
	if replies[0].Error != nil {
		t.Fatalf("initialize failed: %+v", replies[0].Error)
	}

	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(replies[1].Result, &listed); err != nil {
		t.Fatalf("tools/list result is not decodable: %v", err)
	}
	if len(listed.Tools) != 14 {
		t.Fatalf("tools/list returned %d tools, want 14", len(listed.Tools))
	}
	if !strings.Contains(stderr.String(), "serving workspace") {
		t.Fatalf("diagnostics should go to stderr, got %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "serving workspace") {
		t.Fatal("diagnostics must never leak into the MCP stdout stream")
	}
}

func TestRunReadsWorkspaceFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOTEVAULT_WORKSPACE", dir)
	var stdout, stderr bytes.Buffer
	requests := `{"jsonrpc":"2.0","id":7,"method":"ping"}` + "\n"
	if code := run(nil, strings.NewReader(requests), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	replies := decodeReplies(t, stdout.String())
	if len(replies) != 1 || string(replies[0].ID) != "7" {
		t.Fatalf("unexpected replies: %s", stdout.String())
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--enable-writ"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay clean, got %q", stdout.String())
	}
}
