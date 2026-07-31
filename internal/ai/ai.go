// Package ai turns a natural-language request into a single shell command.
//
// The command is never executed here. It is returned to the browser, typed into
// the terminal, and left for the user to review and press Enter on. That
// keeps a wrong or destructive suggestion from doing damage on its own.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	defaultModel   = "claude-haiku-4-5-20251001"
	apiVersion     = "2023-06-01"
	maxTokens      = 400
	requestTimeout = 30 * time.Second
)

// systemPrompt keeps the model to one line of shell and no prose. Anything
// chattier is unusable as terminal input.
const systemPrompt = `You translate a user's natural-language request into ONE shell command for GNU/Linux bash.

Rules:
- Reply with JSON only: {"command": "...", "explain": "..."}
- "command" is a single line of bash, ready to run. No markdown, no backticks, no leading $.
- "explain" is at most 20 characters, in the same language the user wrote in.
- If the request needs several steps, chain them with && or |.
- If the request is destructive (rm, dd, mkfs, chmod -R, kill -9, truncating redirects, writes under /etc /boot /dev /sys), still return the command but start "explain" with "危险: ".
- If the request is not a shell task or is too vague to turn into a command, return {"command": "", "explain": "<short reason>"}.
- Prefer widely available tools. Do not invent flags.`

type Handler struct {
	client  *http.Client
	baseURL string
	token   string
	model   string
}

// NewHandler reads credentials from the environment. ANTHROPIC_AUTH_TOKEN or
// ANTHROPIC_API_KEY enables the feature; with neither set the endpoint reports
// that it is disabled rather than failing obscurely per request.
func NewHandler() *Handler {
	token := os.Getenv("ANTHROPIC_AUTH_TOKEN")
	if token == "" {
		token = os.Getenv("ANTHROPIC_API_KEY")
	}

	baseURL := strings.TrimSuffix(os.Getenv("ANTHROPIC_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	model := os.Getenv("SHELLFB_AI_MODEL")
	if model == "" {
		model = defaultModel
	}

	return &Handler{
		client:  &http.Client{Timeout: requestTimeout},
		baseURL: baseURL,
		token:   token,
		model:   model,
	}
}

func (h *Handler) Enabled() bool { return h.token != "" }

type translateRequest struct {
	Text string `json:"text"`
	Cwd  string `json:"cwd"`
	OS   string `json:"os"`
}

type translateResponse struct {
	Command string `json:"command"`
	Explain string `json:"explain"`
	Error   string `json:"error,omitempty"`
}

// HandleStatus lets the frontend hide the AI controls when no key is configured.
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": h.Enabled(),
		"model":   h.model,
	})
}

func (h *Handler) HandleTranslate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, translateResponse{
			Error: "AI 未配置：请设置 ANTHROPIC_AUTH_TOKEN",
		})
		return
	}

	var req translateRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, translateResponse{Error: "invalid request"})
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		writeJSON(w, http.StatusBadRequest, translateResponse{Error: "empty text"})
		return
	}

	cmd, explain, err := h.translate(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, translateResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, translateResponse{Command: cmd, Explain: explain})
}

type apiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system"`
	Messages  []apiMessage `json:"messages"`
}

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type apiResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (h *Handler) translate(ctx context.Context, req translateRequest) (string, string, error) {
	var b strings.Builder
	b.WriteString("Request: ")
	b.WriteString(req.Text)
	if req.Cwd != "" {
		b.WriteString("\nCurrent directory: ")
		b.WriteString(req.Cwd)
	}
	if req.OS != "" {
		b.WriteString("\nSystem: ")
		b.WriteString(req.OS)
	}

	payload, err := json.Marshal(apiRequest{
		Model:     h.model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  []apiMessage{{Role: "user", Content: b.String()}},
	})
	if err != nil {
		return "", "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return "", "", err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("anthropic-version", apiVersion)
	// Anthropic uses x-api-key; some gateways only read Authorization. Send both.
	httpReq.Header.Set("x-api-key", h.token)
	httpReq.Header.Set("authorization", "Bearer "+h.token)

	resp, err := h.client.Do(httpReq)
	if err != nil {
		return "", "", fmt.Errorf("AI 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", err
	}

	var ar apiResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return "", "", fmt.Errorf("AI 返回无法解析 (HTTP %d)", resp.StatusCode)
	}
	if ar.Error != nil {
		return "", "", errors.New("AI: " + ar.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("AI 返回 HTTP %d", resp.StatusCode)
	}

	var text string
	for _, c := range ar.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return parseResult(text)
}

// parseResult pulls the JSON object out of the model's reply. Models sometimes
// wrap it in a markdown fence despite instructions, so the braces are located
// rather than assuming the whole reply is JSON.
func parseResult(text string) (string, string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", errors.New("AI 返回为空")
	}

	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return "", "", errors.New("AI 未返回可用命令")
	}

	var out struct {
		Command string `json:"command"`
		Explain string `json:"explain"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return "", "", errors.New("AI 返回格式错误")
	}

	// A command must stay one line: a stray newline would submit it early and
	// run whatever follows without review.
	cmd := strings.TrimSpace(out.Command)
	if i := strings.IndexAny(cmd, "\r\n"); i >= 0 {
		cmd = strings.TrimSpace(cmd[:i])
	}
	cmd = strings.TrimPrefix(cmd, "$ ")

	return cmd, strings.TrimSpace(out.Explain), nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
