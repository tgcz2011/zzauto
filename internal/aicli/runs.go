// runs.go 封装 aiclibridge /v1/runs SSE 流式接口与 /v1/runs/{id} 详情接口。
//
// /v1/runs POST 以 SSE 形式返回 run 事件流（thinking/text/tool_use/tool_result/result/error/system）。
// /v1/runs/{id} GET 返回单个 run 的完整事件时间线。供 v0.3.0 任务面板使用。
package aicli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RunEvent 表示一个 run 流事件。
type RunEvent struct {
	Type      string `json:"type"`       // thinking / text / tool_use / tool_result / result / error / system
	Content   string `json:"content"`    // 文本内容（thinking/text）
	ToolName  string `json:"tool_name"`  // tool_use 时
	ToolInput string `json:"tool_input"` // tool_use 时
	RunID     string `json:"run_id"`     // 系统/结果事件含
	Error     string `json:"error"`      // error 事件
}

// RunDetail 单个 run 的完整信息。
type RunDetail struct {
	ID        string     `json:"id"`
	Model     string     `json:"model"`
	Status    string     `json:"status"`
	CreatedAt string     `json:"created_at"`
	Events    []RunEvent `json:"events"`
}

// RunStream 发起一次 run，按 SSE 事件回调 onEvent。
// 返回最终的 runID（从 system/result 事件提取）。
// model 为空时用 c.model。
// onEvent 在每个解析出的事件上同步调用；返回 error 时立即终止流。
// SSE 协议：data: <json>\n\n
func (c *Client) RunStream(ctx context.Context, model, system, user string, onEvent func(RunEvent) error) (string, error) {
	if model == "" {
		model = c.model
	}
	body := map[string]any{
		"model":    model,
		"system":   system,
		"messages": []map[string]string{{"role": "user", "content": user}},
		"stream":   true,
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/v1/runs", bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("构造 run 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 /v1/runs 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("/v1/runs 返回 %d: %s", resp.StatusCode, string(b))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var runID string
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var evt RunEvent
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue // 跳过无法解析的事件
		}
		if evt.RunID != "" && runID == "" {
			runID = evt.RunID
		}
		if onEvent != nil {
			if err := onEvent(evt); err != nil {
				return runID, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return runID, fmt.Errorf("读取 SSE 流失败: %w", err)
	}
	return runID, nil
}

// GetRun 拉取指定 run 的详情（含完整事件时间线）。
func (c *Client) GetRun(ctx context.Context, runID string) (*RunDetail, error) {
	var detail RunDetail
	if err := c.getJSON(ctx, "/v1/runs/"+runID, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}
