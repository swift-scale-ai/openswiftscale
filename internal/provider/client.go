package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/swift-scale-ai/OpenSwiftScale/internal/catalog"
	"github.com/swift-scale-ai/OpenSwiftScale/internal/router"
)

var ErrUnsupportedEndpoint = errors.New("provider does not support endpoint")

type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client, _ ...map[string]string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 120 * time.Second}
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) Do(ctx context.Context, endpoint string, route router.Route, body []byte, source http.Header) (*http.Response, error) {
	var err error
	p := route.Provider
	path := endpointPath(p, endpoint)
	if path == "" {
		return nil, ErrUnsupportedEndpoint
	}
	if p.Type == "anthropic" && endpoint != "chat" {
		return nil, ErrUnsupportedEndpoint
	}
	if p.Type == "anthropic" {
		body, err = openAIToAnthropic(body, route.Model.UpstreamModel)
		if err != nil {
			return nil, err
		}
	} else {
		var contentType string
		body, contentType, err = rewriteRequestBody(body, source.Get("Content-Type"), route.Model.UpstreamModel)
		if err != nil {
			return nil, err
		}
		if contentType != "" {
			source = source.Clone()
			source.Set("Content-Type", contentType)
		}
	}
	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid provider URL: %w", err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	copyRequestHeaders(req.Header, source)
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	key := route.APIKey
	switch p.Authentication {
	case "api-key", "x-api-key":
		header := p.APIKeyHeader
		if header == "" {
			header = "x-api-key"
		}
		req.Header.Set(header, key)
	default:
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if p.Type == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	return c.httpClient.Do(req)
}

// Probe performs an explicit, read-only connectivity and credential check.
// Compatible providers are expected to expose a models endpoint; no inference
// request is generated and therefore the probe does not consume model tokens.
func (c *Client) Probe(ctx context.Context, route router.Route) (*http.Response, error) {
	base, err := url.Parse(route.Provider.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid provider URL: %w", err)
	}
	path := strings.TrimRight(base.Path, "/")
	if !strings.HasSuffix(path, "/v1") {
		path += "/v1"
	}
	base.Path = path + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, err
	}
	key := route.APIKey
	switch route.Provider.Authentication {
	case "api-key", "x-api-key":
		header := route.Provider.APIKeyHeader
		if header == "" {
			header = "x-api-key"
		}
		req.Header.Set(header, key)
	default:
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if route.Provider.Type == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	return c.httpClient.Do(req)
}

func endpointPath(p catalog.Provider, endpoint string) string {
	switch endpoint {
	case "chat":
		return p.ChatPath
	case "responses":
		return p.ResponsesPath
	case "embeddings":
		return p.EmbeddingsPath
	case "image":
		return p.ImagesPath
	case "rerank":
		return p.RerankPath
	case "video":
		return p.VideosPath
	case "speech":
		return p.SpeechPath
	case "transcription":
		return p.TranscriptionsPath
	default:
		return ""
	}
}

func RetryableStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests || status >= 500
}

func (c *Client) CopyResponse(w http.ResponseWriter, resp *http.Response, route router.Route, requestID string) (Usage, error) {
	defer resp.Body.Close()
	if route.Provider.Type == "anthropic" {
		return copyAnthropicResponse(w, resp, route.Model.ID, requestID)
	}
	copyResponseHeaders(w.Header(), resp.Header)
	w.Header().Set("X-OpenSwiftScale-Request-ID", requestID)
	if isEventStream(resp.Header) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(resp.StatusCode)
		return copyOpenAIStream(w, resp.Body)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Usage{}, err
	}
	w.WriteHeader(resp.StatusCode)
	_, writeErr := w.Write(body)
	return usageFromOpenAI(body), writeErr
}

func rewriteModel(body []byte, model string) []byte {
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	encoded, _ := json.Marshal(model)
	payload["model"] = encoded
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return rewritten
}

func rewriteRequestBody(body []byte, contentType, model string) ([]byte, string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		return rewriteModel(body, model), contentType, nil
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var output bytes.Buffer
	writer := multipart.NewWriter(&output)
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, "", fmt.Errorf("read multipart request: %w", nextErr)
		}
		payload, readErr := io.ReadAll(part)
		if readErr != nil {
			return nil, "", fmt.Errorf("read multipart field: %w", readErr)
		}
		if part.FormName() == "model" {
			payload = []byte(model)
		}
		target, err := writer.CreatePart(part.Header)
		if err != nil {
			return nil, "", fmt.Errorf("create multipart field: %w", err)
		}
		if _, err = target.Write(payload); err != nil {
			return nil, "", fmt.Errorf("write multipart field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart request: %w", err)
	}
	return output.Bytes(), writer.FormDataContentType(), nil
}

func usageFromOpenAI(body []byte) Usage {
	var payload struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			InputTokens      int `json:"input_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			OutputTokens     int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return Usage{}
	}
	if payload.Usage.PromptTokens == 0 {
		payload.Usage.PromptTokens = payload.Usage.InputTokens
	}
	if payload.Usage.CompletionTokens == 0 {
		payload.Usage.CompletionTokens = payload.Usage.OutputTokens
	}
	return Usage{PromptTokens: payload.Usage.PromptTokens, CompletionTokens: payload.Usage.CompletionTokens}
}

func copyOpenAIStream(w http.ResponseWriter, body io.Reader) (Usage, error) {
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	var usage Usage
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			candidate := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if candidate != "[DONE]" {
				current := usageFromOpenAI([]byte(candidate))
				if current.PromptTokens > usage.PromptTokens {
					usage.PromptTokens = current.PromptTokens
				}
				if current.CompletionTokens > usage.CompletionTokens {
					usage.CompletionTokens = current.CompletionTokens
				}
			}
		}
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return usage, err
		}
		if line == "" && flusher != nil {
			flusher.Flush()
		}
	}
	return usage, scanner.Err()
}

func copyRequestHeaders(dst, src http.Header) {
	for key, values := range src {
		switch strings.ToLower(key) {
		case "authorization", "connection", "content-length", "host", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		switch strings.ToLower(key) {
		case "connection", "content-length", "content-encoding", "transfer-encoding":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func isEventStream(header http.Header) bool {
	return strings.Contains(strings.ToLower(header.Get("Content-Type")), "text/event-stream")
}
