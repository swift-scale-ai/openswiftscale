package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func openAIToAnthropic(body []byte, model string) ([]byte, error) {
	var input struct {
		Messages            []map[string]any `json:"messages"`
		MaxTokens           int              `json:"max_tokens"`
		MaxCompletionTokens int              `json:"max_completion_tokens"`
		Temperature         any              `json:"temperature,omitempty"`
		TopP                any              `json:"top_p,omitempty"`
		Stop                any              `json:"stop,omitempty"`
		Stream              bool             `json:"stream"`
		Tools               []struct {
			Type     string         `json:"type"`
			Function map[string]any `json:"function"`
		} `json:"tools,omitempty"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		return nil, fmt.Errorf("invalid JSON request: %w", err)
	}
	maxTokens := input.MaxTokens
	if maxTokens == 0 {
		maxTokens = input.MaxCompletionTokens
	}
	if maxTokens == 0 {
		maxTokens = 4096
	}
	var system any
	var messages []map[string]any
	for _, message := range input.Messages {
		if role, _ := message["role"].(string); role == "system" {
			system = message["content"]
			continue
		}
		messages = append(messages, openAIMessageToAnthropic(message))
	}
	payload := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   messages,
		"stream":     input.Stream,
	}
	if system != nil {
		payload["system"] = system
	}
	if input.Temperature != nil {
		payload["temperature"] = input.Temperature
	}
	if input.TopP != nil {
		payload["top_p"] = input.TopP
	}
	if input.Stop != nil {
		payload["stop_sequences"] = input.Stop
	}
	if len(input.Tools) > 0 {
		tools := make([]map[string]any, 0, len(input.Tools))
		for _, tool := range input.Tools {
			tools = append(tools, map[string]any{
				"name":         tool.Function["name"],
				"description":  tool.Function["description"],
				"input_schema": tool.Function["parameters"],
			})
		}
		payload["tools"] = tools
	}
	return json.Marshal(payload)
}

func openAIMessageToAnthropic(message map[string]any) map[string]any {
	role, _ := message["role"].(string)
	if role == "tool" {
		return map[string]any{
			"role": "user",
			"content": []any{map[string]any{
				"type": "tool_result", "tool_use_id": message["tool_call_id"], "content": message["content"],
			}},
		}
	}
	if role != "assistant" {
		return message
	}
	toolCalls, _ := message["tool_calls"].([]any)
	if len(toolCalls) == 0 {
		return message
	}
	var content []any
	if text, _ := message["content"].(string); text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	for _, raw := range toolCalls {
		call, _ := raw.(map[string]any)
		function, _ := call["function"].(map[string]any)
		arguments := map[string]any{}
		if encoded, _ := function["arguments"].(string); encoded != "" {
			_ = json.Unmarshal([]byte(encoded), &arguments)
		}
		content = append(content, map[string]any{
			"type": "tool_use", "id": call["id"], "name": function["name"], "input": arguments,
		})
	}
	return map[string]any{"role": "assistant", "content": content}
}

func copyAnthropicResponse(w http.ResponseWriter, resp *http.Response, model, requestID string) (Usage, error) {
	w.Header().Set("X-OpenSwiftScale-Request-ID", requestID)
	if resp.StatusCode >= 400 {
		copyResponseHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, err := io.Copy(w, resp.Body)
		return Usage{}, err
	}
	if isEventStream(resp.Header) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(resp.StatusCode)
		return copyAnthropicStream(w, resp.Body, model, requestID)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Usage{}, err
	}
	converted, usage, err := anthropicToOpenAI(body, model, requestID)
	if err != nil {
		return Usage{}, err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, err = w.Write(converted)
	return usage, err
}

func anthropicToOpenAI(body []byte, model, requestID string) ([]byte, Usage, error) {
	var input struct {
		ID         string `json:"id"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		return nil, Usage{}, err
	}
	var text strings.Builder
	var toolCalls []map[string]any
	for _, block := range input.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			arguments, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, map[string]any{
				"id": block.ID, "type": "function",
				"function": map[string]any{"name": block.Name, "arguments": string(arguments)},
			})
		}
	}
	message := map[string]any{"role": "assistant", "content": text.String()}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	finish := "stop"
	if input.StopReason == "tool_use" {
		finish = "tool_calls"
	} else if input.StopReason == "max_tokens" {
		finish = "length"
	}
	id := input.ID
	if id == "" {
		id = requestID
	}
	output := map[string]any{
		"id": id, "object": "chat.completion", "created": time.Now().Unix(), "model": model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": input.Usage.InputTokens, "completion_tokens": input.Usage.OutputTokens, "total_tokens": input.Usage.InputTokens + input.Usage.OutputTokens},
	}
	encoded, err := json.Marshal(output)
	return encoded, Usage{PromptTokens: input.Usage.InputTokens, CompletionTokens: input.Usage.OutputTokens}, err
}

func copyAnthropicStream(w http.ResponseWriter, body io.Reader, model, requestID string) (Usage, error) {
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	usage := Usage{}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var event struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Message struct {
				Usage struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		if event.Message.Usage.InputTokens > 0 {
			usage.PromptTokens = event.Message.Usage.InputTokens
		}
		if event.Usage.OutputTokens > 0 {
			usage.CompletionTokens = event.Usage.OutputTokens
		}
		var delta map[string]any
		finish := any(nil)
		switch {
		case event.Type == "content_block_delta" && event.Delta.Type == "text_delta":
			delta = map[string]any{"content": event.Delta.Text}
		case event.Type == "content_block_start" && event.ContentBlock.Type == "tool_use":
			delta = map[string]any{"tool_calls": []any{map[string]any{"index": event.Index, "id": event.ContentBlock.ID, "type": "function", "function": map[string]any{"name": event.ContentBlock.Name, "arguments": ""}}}}
		case event.Type == "content_block_delta" && event.Delta.Type == "input_json_delta":
			delta = map[string]any{"tool_calls": []any{map[string]any{"index": event.Index, "function": map[string]any{"arguments": event.Delta.PartialJSON}}}}
		case event.Type == "message_delta":
			delta = map[string]any{}
			finish = mapAnthropicStop(event.Delta.StopReason)
		case event.Type == "message_stop":
			if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
				return usage, err
			}
			if flusher != nil {
				flusher.Flush()
			}
			continue
		default:
			continue
		}
		chunk := map[string]any{
			"id": requestID, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}
		encoded, _ := json.Marshal(chunk)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", encoded); err != nil {
			return usage, err
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	return usage, scanner.Err()
}

func mapAnthropicStop(reason string) string {
	switch reason {
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return "stop"
	}
}
