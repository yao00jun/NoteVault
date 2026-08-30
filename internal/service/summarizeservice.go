package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SummarizeService 提供 AI 笔记总结能力
// 设计：调用 OpenAI 兼容的 Chat Completions API（任意兼容端点均可，如 OpenAI / DeepSeek / 本地 Ollama）。
// API Key 仅由前端在调用时传入，Go 端不落盘存储，保障隐私。
type SummarizeService struct {
	client *http.Client
}

// NewSummarizeService 创建总结服务实例
func NewSummarizeService() *SummarizeService {
	return &SummarizeService{
		client: &http.Client{Timeout: 90 * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Summarize 调用大模型生成笔记摘要
//   - apiKey: 用户 API Key（本地存储，不落盘）
//   - baseURL: 接口地址，例如 https://api.openai.com/v1 或 https://api.deepseek.com/v1
//   - model: 模型名称
//   - content: 待总结的笔记正文
//
// 返回 Markdown 格式的摘要文本。
func (s *SummarizeService) Summarize(apiKey, baseURL, model, content string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	if strings.TrimSpace(apiKey) == "" {
		return "", fmt.Errorf("请先在「设置 → AI 总结」中填写 API Key")
	}
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("没有可总结的内容")
	}

	// 限制输入长度，避免超出上下文窗口
	const maxRunes = 12000
	runes := []rune(content)
	if len(runes) > maxRunes {
		content = string(runes[:maxRunes]) + "\n\n（内容过长已自动截断）"
	}

	systemPrompt := "你是一个专业的笔记总结助手。请用简洁的中文总结用户提供的笔记内容，" +
		"提炼 3-5 个核心要点，使用 Markdown 无序列表输出，必要时可补充一句话总括。" +
		"只输出总结内容，不要添加额外解释或客套话。"

	reqBody := chatRequest{
		Model:  model,
		Stream: false,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: content},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("构造请求失败：%w", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构造请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 AI 服务失败（请检查网络与 BaseURL）：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respData, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI 服务返回错误（%d）：%s", resp.StatusCode, string(respData))
	}

	var cr chatResponse
	if err := json.Unmarshal(respData, &cr); err != nil {
		return "", fmt.Errorf("解析 AI 响应失败：%w", err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", fmt.Errorf("AI 服务错误：%s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("AI 服务未返回内容")
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), nil
}
