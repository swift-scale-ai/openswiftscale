package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swift-scale-ai/OpenSwiftScale/internal/catalog"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/router"
)

func TestOpenAICompatibleRequestAndUsage(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer provider-key" {
			t.Errorf("unexpected provider authorization: %q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "upstream-model" {
			t.Errorf("model was not rewritten: %#v", body["model"])
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":8}}`)), Request: r}, nil
	})
	route := router.Route{Provider: catalog.Provider{ID: "example", Type: "openai-compatible", BaseURL: "https://provider.example", ChatPath: "/v1/chat/completions"}, Model: catalog.Model{ID: "public-model", UpstreamModel: "upstream-model"}, APIKey: "provider-key"}
	client := NewClient(&http.Client{Transport: transport})
	response, err := client.Do(context.Background(), "chat", route, []byte(`{"model":"public-model","messages":[]}`), http.Header{"Authorization": []string{"Bearer client-key"}})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	usage, err := client.CopyResponse(recorder, response, route, "request-id")
	if err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens != 12 || usage.CompletionTokens != 8 {
		t.Fatalf("unexpected usage: %#v", usage)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestAnthropicTranslation(t *testing.T) {
	converted, err := openAIToAnthropic([]byte(`{"model":"public","messages":[{"role":"system","content":"safe"},{"role":"user","content":"hello"}],"max_tokens":99}`), "claude-upstream")
	if err != nil {
		t.Fatal(err)
	}
	text := string(converted)
	for _, expected := range []string{`"model":"claude-upstream"`, `"system":"safe"`, `"max_tokens":99`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %s in %s", expected, text)
		}
	}
	output, usage, err := anthropicToOpenAI([]byte(`{"id":"msg_1","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":4}}`), "claude", "req")
	if err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens != 3 || usage.CompletionTokens != 4 || !strings.Contains(string(output), `"content":"hello"`) {
		t.Fatalf("unexpected response: %s %#v", output, usage)
	}
}

func TestAnthropicToolMessages(t *testing.T) {
	converted, err := openAIToAnthropic([]byte(`{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"id\":7}"}}]},{"role":"tool","tool_call_id":"call_1","content":"found"}],"max_tokens":50}`), "claude")
	if err != nil {
		t.Fatal(err)
	}
	text := string(converted)
	for _, expected := range []string{`"type":"tool_use"`, `"name":"lookup"`, `"type":"tool_result"`, `"tool_use_id":"call_1"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %s in %s", expected, text)
		}
	}
}
