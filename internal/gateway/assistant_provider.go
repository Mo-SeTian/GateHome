package gateway

import (
	"bufio"
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

func (e assistantEndpoint) newRequest(ctx context.Context, method, path string, payload any, accept string) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.Base+path, body)
	if err != nil {
		return nil, errors.New("模型接口地址无效")
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
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
	return req, nil
}

func (e assistantEndpoint) request(ctx context.Context, client *http.Client, method, path string, payload any, result any) error {
	req, err := e.newRequest(ctx, method, path, payload, "application/json")
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
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

func (e assistantEndpoint) listModels(ctx context.Context, client *http.Client) ([]string, error) {
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
		if err := e.request(ctx, client, http.MethodGet, path, nil, &result); err != nil {
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

type assistantCompletionResult struct {
	Choices []struct {
		Message assistantMessage `json:"message"`
	} `json:"choices"`
	Output []struct {
		Content []assistantTextBlock `json:"content"`
	} `json:"output"`
	Content []assistantTextBlock `json:"content"`
}

func normalizeAnthropicMessages(messages []assistantMessage) ([]assistantMessage, error) {
	out := make([]assistantMessage, 0, len(messages))
	for _, msg := range messages {
		if len(out) == 0 && msg.Role == "assistant" {
			continue
		}
		if len(out) > 0 && out[len(out)-1].Role == msg.Role {
			out[len(out)-1].Content += "\n\n" + msg.Content
			continue
		}
		out = append(out, msg)
	}
	if len(out) == 0 || out[0].Role != "user" {
		return nil, errors.New("对话格式无效")
	}
	return out, nil
}

func (e assistantEndpoint) buildCompletionRequest(messages []assistantMessage, stream bool) (string, map[string]any, error) {
	payload := map[string]any{"model": e.Model}
	path := "/chat/completions"
	switch e.protocol() {
	case "responses":
		path = "/responses"
		payload["input"] = messages
		payload["store"] = false
		if stream {
			payload["stream"] = true
		}
		if e.Effort != "" {
			payload["reasoning"] = map[string]string{"effort": e.Effort}
		}
	case "anthropic":
		path = "/messages"
		if len(messages) > 0 && messages[0].Role == "system" {
			payload["system"], messages = messages[0].Content, messages[1:]
		}
		normalized, err := normalizeAnthropicMessages(messages)
		if err != nil {
			return "", nil, err
		}
		payload["messages"] = normalized
		payload["max_tokens"] = 8192
		if stream {
			payload["stream"] = true
		}
		if e.Effort != "" {
			payload["thinking"] = map[string]string{"type": "adaptive"}
			payload["output_config"] = map[string]string{"effort": e.Effort}
		}
	default:
		payload["messages"] = messages
		payload["stream"] = stream
		if e.Effort != "" {
			payload["reasoning_effort"] = e.Effort
		}
	}
	return path, payload, nil
}

func (e assistantEndpoint) finalizeContent(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" || len(content) > 20000 {
		return "", errors.New("模型回复为空或过长")
	}
	if e.Key != "" {
		content = strings.ReplaceAll(content, e.Key, "<REDACTED>")
	}
	return content, nil
}

func (e assistantEndpoint) extractCompletionContent(result assistantCompletionResult) (string, error) {
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
	return e.finalizeContent(strings.Join(parts, "\n"))
}

func (e assistantEndpoint) complete(ctx context.Context, client *http.Client, messages []assistantMessage) (string, error) {
	path, payload, err := e.buildCompletionRequest(messages, false)
	if err != nil {
		return "", err
	}
	var result assistantCompletionResult
	if err := e.request(ctx, client, http.MethodPost, path, payload, &result); err != nil {
		return "", err
	}
	return e.extractCompletionContent(result)
}

func (e assistantEndpoint) parseStreamEvent(data string) (delta string, done bool, finalContent string, err error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return "", false, "", nil
	}
	if data == "[DONE]" {
		return "", true, "", nil
	}
	switch e.protocol() {
	case "responses":
		var event struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Response struct {
				Output []struct {
					Content []assistantTextBlock `json:"content"`
				} `json:"output"`
			} `json:"response"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			return "", false, "", nil
		}
		if event.Type == "error" || event.Error != nil {
			return "", false, "", errors.New("模型接口返回流式错误，请检查接入设置")
		}
		switch event.Type {
		case "response.output_text.delta":
			return event.Delta, false, "", nil
		case "response.completed":
			var parts []string
			for _, row := range event.Response.Output {
				for _, block := range row.Content {
					if block.Type == "output_text" {
						parts = append(parts, block.Text)
					}
				}
			}
			return "", true, strings.Join(parts, "\n"), nil
		}
	case "anthropic":
		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			ContentBlock *assistantTextBlock `json:"content_block"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			return "", false, "", nil
		}
		switch event.Type {
		case "error":
			return "", false, "", errors.New("模型接口返回流式错误，请检查接入设置")
		case "content_block_delta":
			if event.Delta.Type == "text_delta" {
				return event.Delta.Text, false, "", nil
			}
		case "content_block_start":
			if event.ContentBlock != nil && event.ContentBlock.Type == "text" {
				return event.ContentBlock.Text, false, "", nil
			}
		case "message_stop":
			return "", true, "", nil
		}
	default:
		var event struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				Message assistantMessage `json:"message"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			return "", false, "", nil
		}
		if event.Error != nil {
			return "", false, "", errors.New("模型接口返回流式错误，请检查接入设置")
		}
		if len(event.Choices) > 0 {
			if event.Choices[0].Delta.Content != "" {
				return event.Choices[0].Delta.Content, false, "", nil
			}
			if event.Choices[0].Message.Content != "" {
				return event.Choices[0].Message.Content, false, "", nil
			}
		}
	}
	return "", false, "", nil
}

func (e assistantEndpoint) completeStream(ctx context.Context, client *http.Client, messages []assistantMessage, onDelta func(string) error) (string, error) {
	path, payload, err := e.buildCompletionRequest(messages, true)
	if err != nil {
		return "", err
	}
	req, err := e.newRequest(ctx, http.MethodPost, path, payload, "text/event-stream, application/json")
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("无法连接模型接口或请求超时，请检查接入设置")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("模型接口返回 HTTP %d，请检查接入设置", resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "text/event-stream") {
		var result assistantCompletionResult
		if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result) != nil {
			return "", errors.New("模型接口响应格式无效")
		}
		content, err := e.extractCompletionContent(result)
		if err != nil {
			return "", err
		}
		if onDelta != nil {
			if err := onDelta(content); err != nil {
				return "", err
			}
		}
		return content, nil
	}

	reader := bufio.NewReaderSize(io.LimitReader(resp.Body, 4<<20), 64<<10)
	var full strings.Builder
	var eventData []string
	var fallbackFinal string
	dispatch := func() (bool, error) {
		if len(eventData) == 0 {
			return false, nil
		}
		joined := strings.Join(eventData, "\n")
		eventData = eventData[:0]
		delta, done, final, parseErr := e.parseStreamEvent(joined)
		if parseErr != nil {
			return false, parseErr
		}
		if final != "" {
			fallbackFinal = final
		}
		if delta != "" {
			if full.Len()+len(delta) > 20000 {
				return false, errors.New("模型回复为空或过长")
			}
			full.WriteString(delta)
			if onDelta != nil {
				if err := onDelta(delta); err != nil {
					return false, err
				}
			}
		}
		return done, nil
	}

	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 256<<10 {
			return "", errors.New("模型流式响应单行过长")
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			done, err := dispatch()
			if err != nil {
				return "", err
			}
			if done {
				break
			}
		} else if strings.HasPrefix(trimmed, "data:") {
			value := strings.TrimPrefix(trimmed, "data:")
			value = strings.TrimPrefix(value, " ")
			eventData = append(eventData, value)
		}
		if readErr != nil {
			if readErr != io.EOF {
				return "", errors.New("模型流式连接已中断，请重试")
			}
			if _, err := dispatch(); err != nil {
				return "", err
			}
			break
		}
	}

	if full.Len() == 0 && fallbackFinal != "" {
		if len(fallbackFinal) > 20000 {
			return "", errors.New("模型回复为空或过长")
		}
		full.WriteString(fallbackFinal)
		if onDelta != nil {
			if err := onDelta(fallbackFinal); err != nil {
				return "", err
			}
		}
	}
	return e.finalizeContent(full.String())
}
