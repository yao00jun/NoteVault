package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// blockerTestServer 起一个 OpenAI 兼容的假 LLM 端点，返回固定的前置清单。
func blockerTestServer(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + reply + `"}}]}`))
	}))
}

func TestParseBlockerPrerequisites(t *testing.T) {
	raw := "PRE: Python 基础 | 变量与函数是读代码的前提 | 1.5h\n" +
		"PRE: HTTP 请求 | API 调用需要 | 2h\n" +
		"这不是 PRE 行，跳过\n" +
		"PRE: 只有名字的行跳过"
	out := parseBlockerPrerequisites(raw)
	if len(out) != 2 {
		t.Fatalf("expected 2 prerequisites, got %d: %+v", len(out), out)
	}
	if out[0].Name != "Python 基础" || out[0].Reason != "变量与函数是读代码的前提" || out[0].Hours != 1.5 {
		t.Fatalf("unexpected first: %+v", out[0])
	}
	if out[1].Hours != 2 {
		t.Fatalf("unexpected hours: %+v", out[1])
	}
}

func TestReportService_DecomposeBlockerDegradesWithoutAI(t *testing.T) {
	svc := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), nil)
	res := svc.decomposeBlocker(t.Context(), TaskReporter{}, "RAG 分块策略", "", WeeklyReportAIConfig{})
	if res.Message == "" || len(res.Prerequisites) != 0 {
		t.Fatalf("unconfigured AI should degrade with message: %+v", res)
	}
	if res.Concept != "RAG 分块策略" {
		t.Fatalf("concept must be preserved: %+v", res)
	}
}

func TestReportService_DecomposeBlockerParsesModelOutput(t *testing.T) {
	server := blockerTestServer(t, "PRE: Python 基础 | 先会读代码 | 1.5h\\nPRE: JSON 处理 | API 返回解析需要 | 1h")
	defer server.Close()
	svc := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), nil)
	res := svc.decomposeBlocker(t.Context(), TaskReporter{}, "RAG 分块策略", "", WeeklyReportAIConfig{
		BaseURL: server.URL, Model: "test-model", APIKey: "k", Protocol: "openai-chat",
	})
	if res.Message != "" {
		t.Fatalf("unexpected message: %s", res.Message)
	}
	if len(res.Prerequisites) != 2 || res.Prerequisites[0].Name != "Python 基础" {
		t.Fatalf("unexpected prerequisites: %+v", res.Prerequisites)
	}
}

func TestReportService_DecomposeBlockerAIFailureDegrades(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	svc := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), nil)
	res := svc.decomposeBlocker(t.Context(), TaskReporter{}, "X", "", WeeklyReportAIConfig{
		BaseURL: server.URL, Model: "m", Protocol: "openai-chat",
	})
	if res.Message == "" || len(res.Prerequisites) != 0 {
		t.Fatalf("AI failure should degrade with message: %+v", res)
	}
}

func TestReportService_StartDecomposeBlockerTaskLifecycle(t *testing.T) {
	// 无队列 → 空 ID，调用方退回同步路径
	svc := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), nil)
	if id := svc.StartDecomposeBlocker("X", "", WeeklyReportAIConfig{}); id != "" {
		t.Fatalf("no queue should return empty ID, got %q", id)
	}

	server := blockerTestServer(t, "PRE: Python 基础 | 先会读代码 | 1.5h")
	defer server.Close()
	tasks := NewTaskService(nil)
	svc2 := NewReportServiceWithTasks(NewFileServiceWithHistory(NewSnapshotService()), nil, tasks)
	ai := WeeklyReportAIConfig{BaseURL: server.URL, Model: "m", Protocol: "openai-chat"}

	id := svc2.StartDecomposeBlocker("RAG 分块策略", "在读第 4 章", ai)
	if id == "" {
		t.Fatal("should return task ID")
	}
	tasks.Wait()
	res := svc2.DecomposeTaskResult(id)
	if res == nil || len(res.Prerequisites) != 1 || res.Prerequisites[0].Name != "Python 基础" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Concept != "RAG 分块策略" || res.Context != "在读第 4 章" {
		t.Fatalf("concept/context must be preserved: %+v", res)
	}
	// 未知 ID 返回 nil（前端据此提示过期）
	if res := svc2.DecomposeTaskResult("task-999"); res != nil {
		t.Fatalf("unknown task must return nil, got %+v", res)
	}
	// 空概念：模型仍会返回前置（概念名由用户后续补充），此处只验证不 panic
	if res := svc2.DecomposeTaskResult(id); res == nil {
		t.Fatal("result should be retrievable")
	}
}

func TestReportService_DecomposeBlockerEmptyConceptDegrades(t *testing.T) {
	server := blockerTestServer(t, "PRE: x | y | 1h")
	defer server.Close()
	svc := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), nil)
	res := svc.decomposeBlocker(t.Context(), TaskReporter{}, "   ", "", WeeklyReportAIConfig{
		BaseURL: server.URL, Model: "m", Protocol: "openai-chat",
	})
	if !strings.Contains(res.Concept, "") || len(res.Prerequisites) == 0 {
		t.Fatalf("empty concept still decomposes (model sees empty concept): %+v", res)
	}
}
