package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// LLMProtocol 标识 AI 生成端点使用的协议/格式。
// 前端「设置 → AI 总结」的下拉与此保持同步。
type LLMProtocol string

const (
	// LLMProtocolOpenAIChat OpenAI 兼容 Chat Completions（/chat/completions）。
	LLMProtocolOpenAIChat LLMProtocol = "openai-chat"
	// LLMProtocolOpenAIResponses OpenAI Responses API（/responses）。
	LLMProtocolOpenAIResponses LLMProtocol = "openai-responses"
	// LLMProtocolAnthropicMessages Anthropic Messages API（/v1/messages）。
	LLMProtocolAnthropicMessages LLMProtocol = "anthropic-messages"
	// LLMProtocolGoogleGemini Google Gemini API（/v1beta/models/{model}:generateContent，key 在 query）。
	LLMProtocolGoogleGemini LLMProtocol = "google-gemini"
	// LLMProtocolGoogleVertex Google Vertex AI（/models/{model}:generateContent，Bearer 鉴权）。
	LLMProtocolGoogleVertex LLMProtocol = "google-vertex"
)

// llmChatComplete 统一调用各协议的 Chat/生成端点，返回模型生成的文本。
//
// 设计取舍：不把 SummarizeService / QnAService 改成依赖某个" LLMClient 对象"，
// 而是保留它们现有的 http.Client 与超时，只把「协议差异」收敛到这个纯函数。
// 这样改动面最小：只新增一个 protocol 参数，不引入新的服务依赖。
func llmChatComplete(
	ctx context.Context,
	client *http.Client,
	apiKey, baseURL, model, protocol, systemPrompt, userPrompt string,
) (string, error) {
	if strings.TrimSpace(model) == "" {
		model = "gpt-4o-mini"
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.openai.com/v1"
	}

	p := LLMProtocol(strings.ToLower(strings.TrimSpace(protocol)))
	if p == "" {
		p = LLMProtocolOpenAIChat
	}

	switch p {
	case LLMProtocolOpenAIChat:
		return llmChatOpenAI(ctx, client, apiKey, baseURL, model, systemPrompt, userPrompt)
	case LLMProtocolOpenAIResponses:
		return llmChatOpenAIResponses(ctx, client, apiKey, baseURL, model, systemPrompt, userPrompt)
	case LLMProtocolAnthropicMessages:
		return llmChatAnthropic(ctx, client, apiKey, baseURL, model, systemPrompt, userPrompt)
	case LLMProtocolGoogleGemini:
		return llmChatGoogleGemini(ctx, client, apiKey, baseURL, model, systemPrompt, userPrompt)
	case LLMProtocolGoogleVertex:
		return llmChatGoogleVertex(ctx, client, apiKey, baseURL, model, systemPrompt, userPrompt)
	default:
		return "", fmt.Errorf("不支持的 AI 协议：%q", protocol)
	}
}

// ---- OpenAI 兼容 Chat Completions ----

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model    string              `json:"model"`
	Messages []openAIChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func llmChatOpenAI(ctx context.Context, client *http.Client, apiKey, baseURL, model, systemPrompt, userPrompt string) (string, error) {
	baseURL = normalizeBaseURL(baseURL)
	reqBody := openAIChatRequest{
		Model:  model,
		Stream: false,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("构造请求失败：%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构造请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(apiKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	respData, status, err := doLLMRequest(client, req)
	if err != nil {
		return "", err
	}
	var cr openAIChatResponse
	if err := json.Unmarshal(respData, &cr); err != nil {
		return "", fmt.Errorf("解析响应失败：%w", err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", fmt.Errorf("AI 服务错误：%s", cr.Error.Message)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("AI 服务返回错误（%d）：%s", status, string(respData))
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("AI 服务未返回内容")
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), nil
}

// ---- OpenAI Responses API ----

type openAIResponseInput struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponsesRequest struct {
	Model string                `json:"model"`
	Input []openAIResponseInput `json:"input"`
}

type openAIResponsesResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func llmChatOpenAIResponses(ctx context.Context, client *http.Client, apiKey, baseURL, model, systemPrompt, userPrompt string) (string, error) {
	baseURL = normalizeBaseURL(baseURL)
	reqBody := openAIResponsesRequest{
		Model: model,
		Input: []openAIResponseInput{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("构造请求失败：%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构造请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(apiKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	respData, status, err := doLLMRequest(client, req)
	if err != nil {
		return "", err
	}
	var rr openAIResponsesResponse
	if err := json.Unmarshal(respData, &rr); err != nil {
		return "", fmt.Errorf("解析响应失败：%w", err)
	}
	if rr.Error != nil && rr.Error.Message != "" {
		return "", fmt.Errorf("AI 服务错误：%s", rr.Error.Message)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("AI 服务返回错误（%d）：%s", status, string(respData))
	}
	if rr.OutputText != "" {
		return strings.TrimSpace(rr.OutputText), nil
	}
	if len(rr.Output) > 0 && len(rr.Output[0].Content) > 0 {
		return strings.TrimSpace(rr.Output[0].Content[0].Text), nil
	}
	return "", fmt.Errorf("AI 服务未返回内容")
}

// ---- Anthropic Messages API ----

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func llmChatAnthropic(ctx context.Context, client *http.Client, apiKey, baseURL, model, systemPrompt, userPrompt string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: userPrompt},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("构造请求失败：%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构造请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if key := strings.TrimSpace(apiKey); key != "" {
		req.Header.Set("x-api-key", key)
	}

	respData, status, err := doLLMRequest(client, req)
	if err != nil {
		return "", err
	}
	var ar anthropicResponse
	if err := json.Unmarshal(respData, &ar); err != nil {
		return "", fmt.Errorf("解析响应失败：%w", err)
	}
	if ar.Error != nil && ar.Error.Message != "" {
		return "", fmt.Errorf("AI 服务错误：%s", ar.Error.Message)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("AI 服务返回错误（%d）：%s", status, string(respData))
	}
	for _, c := range ar.Content {
		if c.Type == "text" {
			return strings.TrimSpace(c.Text), nil
		}
	}
	return "", fmt.Errorf("AI 服务未返回内容")
}

// ---- Google Gemini / Vertex ----

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func llmChatGoogleGemini(ctx context.Context, client *http.Client, apiKey, baseURL, model, systemPrompt, userPrompt string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	url := fmt.Sprintf("%s/models/%s:generateContent", baseURL, model)
	reqBody := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: userPrompt}}},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("构造请求失败：%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构造请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Key 走请求头而不是 URL：http 客户端的错误信息内嵌完整 URL，
	// 拼 URL 会让一次 DNS 失败就把 Key 泄漏到前端错误提示/日志里
	req.Header.Set("x-goog-api-key", apiKey)

	respData, status, err := doLLMRequest(client, req)
	if err != nil {
		return "", err
	}
	var gr geminiResponse
	if err := json.Unmarshal(respData, &gr); err != nil {
		return "", fmt.Errorf("解析响应失败：%w", err)
	}
	if gr.Error != nil && gr.Error.Message != "" {
		return "", fmt.Errorf("AI 服务错误：%s", gr.Error.Message)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("AI 服务返回错误（%d）：%s", status, string(respData))
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("AI 服务未返回内容")
	}
	return strings.TrimSpace(gr.Candidates[0].Content.Parts[0].Text), nil
}

func llmChatGoogleVertex(ctx context.Context, client *http.Client, apiKey, baseURL, model, systemPrompt, userPrompt string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("Vertex AI 必须填写 Base URL（含项目/区域）")
	}
	url := fmt.Sprintf("%s/models/%s:generateContent", baseURL, model)
	reqBody := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: userPrompt}}},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("构造请求失败：%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构造请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(apiKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	respData, status, err := doLLMRequest(client, req)
	if err != nil {
		return "", err
	}
	var gr geminiResponse
	if err := json.Unmarshal(respData, &gr); err != nil {
		return "", fmt.Errorf("解析响应失败：%w", err)
	}
	if gr.Error != nil && gr.Error.Message != "" {
		return "", fmt.Errorf("AI 服务错误：%s", gr.Error.Message)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("AI 服务返回错误（%d）：%s", status, string(respData))
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("AI 服务未返回内容")
	}
	return strings.TrimSpace(gr.Candidates[0].Content.Parts[0].Text), nil
}

// ---- 通用 HTTP 辅助 ----

func doLLMRequest(client *http.Client, req *http.Request) ([]byte, int, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("请求 AI 服务失败（请检查网络与 BaseURL）：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return data, resp.StatusCode, nil
}
