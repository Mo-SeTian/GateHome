package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

func (e assistantEndpoint) request(ctx context.Context, method, path string, payload any, result any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.Base+path, body)
	if err != nil {
		return errors.New("模型接口地址无效")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if e.protocol() == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
		if e.Key != "" {
			req.Header.Set("x-api-key", e.Key)
		}
	} else if e.Key != "" {
		req.Header.Set("Authorization", "Bearer "+e.Key)
	}
	resp, err := assistantHTTP.Do(req)
	if err != nil {
		return errors.New("无法连接模型接口或请求超时，请检查接入设置")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("模型接口返回 HTTP %d，请检查接入设置", resp.StatusCode)
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(result) != nil {
		return errors.New("模型接口响应格式无效")
	}
	return nil
}

func (e assistantEndpoint) listModels(ctx context.Context) ([]string, error) {
	models := []string{}
	cursor := ""
	for range 5 {
		path := "/models"
		if cursor != "" {
			path += "?after_id=" + url.QueryEscape(cursor)
		}
		var result struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := e.request(ctx, http.MethodGet, path, nil, &result); err != nil {
			return nil, err
		}
		for _, model := range result.Data {
			id := strings.TrimSpace(model.ID)
			if id != "" && len(id) <= 200 && (e.Key == "" || !strings.Contains(id, e.Key)) && !slices.Contains(models, id) {
				models = append(models, id)
			}
			if len(models) == 300 {
				break
			}
		}
		if len(models) >= 300 || e.protocol() != "anthropic" || !result.HasMore || result.LastID == "" || result.LastID == cursor {
			break
		}
		cursor = result.LastID
	}
	slices.Sort(models)
	return models, nil
}

type assistantTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (e assistantEndpoint) complete(ctx context.Context, messages []assistantMessage) (string, error) {
	payload := map[string]any{"model": e.Model}
	path := "/chat/completions"
	var result struct {
		Choices []struct {
			Message assistantMessage `json:"message"`
		} `json:"choices"`
		Output []struct {
			Content []assistantTextBlock `json:"content"`
		} `json:"output"`
		Content []assistantTextBlock `json:"content"`
	}
	switch e.protocol() {
	case "responses":
		path = "/responses"
		payload["input"] = messages
		payload["store"] = false
		if e.Effort != "" {
			payload["reasoning"] = map[string]string{"effort": e.Effort}
		}
	case "anthropic":
		path = "/messages"
		if messages[0].Role == "system" {
			payload["system"], messages = messages[0].Content, messages[1:]
		}
		// Claude conversations start with a user message.
		if messages[0].Role == "assistant" {
			messages = messages[1:]
		}
		payload["messages"] = messages
		payload["max_tokens"] = 8192
		if e.Effort != "" {
			payload["thinking"] = map[string]string{"type": "adaptive"}
			payload["output_config"] = map[string]string{"effort": e.Effort}
		}
	default:
		payload["messages"] = messages
		payload["stream"] = false
		if e.Effort != "" {
			payload["reasoning_effort"] = e.Effort
		}
	}
	if err := e.request(ctx, http.MethodPost, path, payload, &result); err != nil {
		return "", err
	}
	var parts []string
	switch e.protocol() {
	case "responses":
		for _, row := range result.Output {
			for _, block := range row.Content {
				if block.Type == "output_text" {
					parts = append(parts, block.Text)
				}
			}
		}
	case "anthropic":
		for _, block := range result.Content {
			if block.Type == "text" {
				parts = append(parts, block.Text)
			}
		}
	default:
		if len(result.Choices) > 0 {
			parts = append(parts, result.Choices[0].Message.Content)
		}
	}
	content := strings.TrimSpace(strings.Join(parts, "\n"))
	if content == "" || len(content) > 20000 {
		return "", errors.New("模型回复为空或过长")
	}
	if e.Key != "" {
		content = strings.ReplaceAll(content, e.Key, "<REDACTED>")
	}
	return content, nil
}
