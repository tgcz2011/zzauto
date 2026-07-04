// stats.go 封装 aiclibridge /v1/stats/* 端点的客户端调用。
//
// 这些端点为 v0.3.0 stats 面板提供数据源：用量、定价、总览、并发。
package aicli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// UsageResp 对应 aiclibridge /v1/stats/usage 响应。
type UsageResp struct {
	Models []ModelUsage `json:"models"`
	Total  TotalUsage   `json:"total"`
}

// ModelUsage 单个模型的用量统计。
type ModelUsage struct {
	Model      string  `json:"model"`
	PromptToks int64   `json:"prompt_tokens"`
	CompToks   int64   `json:"completion_tokens"`
	TotalToks  int64   `json:"total_tokens"`
	Requests   int64   `json:"requests"`
	USD        float64 `json:"usd"`
}

// TotalUsage 所有模型汇总用量。
type TotalUsage struct {
	PromptToks int64   `json:"prompt_tokens"`
	CompToks   int64   `json:"completion_tokens"`
	TotalToks  int64   `json:"total_tokens"`
	Requests   int64   `json:"requests"`
	USD        float64 `json:"usd"`
}

// PricesResp 对应 /v1/stats/prices 响应。
type PricesResp struct {
	Models []ModelPrice `json:"models"`
}

// ModelPrice 单个模型的定价（每 1M token）。
type ModelPrice struct {
	Model       string  `json:"model"`
	PromptPer1M float64 `json:"prompt_per_1m"`
	CompPer1M   float64 `json:"completion_per_1m"`
}

// SummaryResp 对应 /v1/stats/summary 响应。
type SummaryResp struct {
	TotalRequests int64   `json:"total_requests"`
	TotalTokens   int64   `json:"total_tokens"`
	TotalUSD      float64 `json:"total_usd"`
	Uptime        string  `json:"uptime"`
}

// ConcurrencyResp 对应 /v1/stats/concurrency 响应。
type ConcurrencyResp struct {
	Active int `json:"active"`
	Queued int `json:"queued"`
	Max    int `json:"max"`
}

// getJSON 发起 GET 请求并将 JSON 响应解析到 v。复用 c.http 与鉴权头。
// 与 doJSON 不同：method 固定 GET、无请求体，避免 nil body 被 Marshal 成 "null"。
func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+path, nil)
	if err != nil {
		return fmt.Errorf("构造 GET 请求失败: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求 aiclibridge %s 失败: %w", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应体失败: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("aiclibridge %s 返回非 2xx: 状态码=%d 响应体=%s", path, resp.StatusCode, string(data))
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("解析 %s 响应失败: %w (body=%s)", path, err, string(data))
	}
	return nil
}

// Usage 拉取用量统计，GET /v1/stats/usage。
func (c *Client) Usage(ctx context.Context) (*UsageResp, error) {
	var resp UsageResp
	if err := c.getJSON(ctx, "/v1/stats/usage", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Prices 拉取模型定价，GET /v1/stats/prices。
func (c *Client) Prices(ctx context.Context) (*PricesResp, error) {
	var resp PricesResp
	if err := c.getJSON(ctx, "/v1/stats/prices", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Summary 拉取总览统计，GET /v1/stats/summary。
func (c *Client) Summary(ctx context.Context) (*SummaryResp, error) {
	var resp SummaryResp
	if err := c.getJSON(ctx, "/v1/stats/summary", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Concurrency 拉取并发信息，GET /v1/stats/concurrency。
func (c *Client) Concurrency(ctx context.Context) (*ConcurrencyResp, error) {
	var resp ConcurrencyResp
	if err := c.getJSON(ctx, "/v1/stats/concurrency", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
