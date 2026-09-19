// Package http exposes a blocking HTTP client to Spore scripts.
// Responses are projected to script maps via the native-value path:
// {status, statusText, ok, contentType, headers, body}.
package http

import (
	"fmt"
	"io"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/qomos-w/spore/binding"
)

const (
	defaultTimeoutMs  int64 = 30000
	maxResponseBytes        = 32 << 20 // 32 MiB upper bound on a response body
)

// Response is the script-facing result shape. Field names are the JSON tags
// the native-value projection uses as map keys.
type Response struct {
	Status      int               `json:"status"`
	StatusText  string            `json:"statusText"`
	OK          bool              `json:"ok"`
	ContentType string            `json:"contentType"`
	Headers     map[string]string `json:"headers"`
	Body        []byte            `json:"body"`
}

// Register registers the http standard library module into the given ScriptBinding.
func Register(sb *binding.ScriptBinding) error {
	builder := binding.NewCapability("http", "module")

	if err := builder.AddFreeFunction("get", func(url string, timeoutMs int64) (*Response, error) {
		return doRequest(nethttp.MethodGet, url, nil, nil, timeoutMs)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("post", func(url string, body any, contentType string, timeoutMs int64) (*Response, error) {
		if contentType == "" {
			contentType = "text/plain"
		}
		headers := map[string]any{"Content-Type": contentType}
		return doRequest(nethttp.MethodPost, url, headers, coerceBody(body), timeoutMs)
	}); err != nil {
		return err
	}

	if err := builder.AddFreeFunction("request", func(method string, url string, headers map[string]any, body any, timeoutMs int64) (*Response, error) {
		return doRequest(strings.ToUpper(method), url, headers, coerceBody(body), timeoutMs)
	}); err != nil {
		return err
	}

	cap, err := builder.Build()
	if err != nil {
		return err
	}
	if err := sb.RegisterCapability(cap); err != nil {
		return err
	}
	return sb.ExposeCapabilityCallables("http")
}

// coerceBody accepts script-provided string or bytes bodies uniformly,
// since the binding layer validates argument types against the declared
// schema before any Go-side coercion runs.
func coerceBody(body any) []byte {
	switch v := body.(type) {
	case string:
		return []byte(v)
	case []byte:
		return v
	default:
		return nil
	}
}

func doRequest(method, url string, headers map[string]any, body []byte, timeoutMs int64) (*Response, error) {
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}
	if url == "" {
		return nil, fmt.Errorf("url is required")
	}
	timeout := defaultTimeoutMs
	if timeoutMs > 0 {
		timeout = timeoutMs
	}

	var reader io.Reader
	if len(body) > 0 {
		reader = strings.NewReader(string(body))
	}
	req, err := nethttp.NewRequest(method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	for k, v := range headers {
		if s, ok := v.(string); ok {
			req.Header.Set(k, s)
			continue
		}
		req.Header.Set(k, fmt.Sprintf("%v", v))
	}

	client := &nethttp.Client{Timeout: time.Duration(timeout) * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	if len(payload) > maxResponseBytes {
		return nil, fmt.Errorf("response body exceeds %d byte limit", maxResponseBytes)
	}

	return &Response{
		Status:      resp.StatusCode,
		StatusText:  resp.Status,
		OK:          resp.StatusCode >= 200 && resp.StatusCode < 300,
		ContentType: resp.Header.Get("Content-Type"),
		Headers:     flattenHeaders(resp.Header),
		Body:        payload,
	}, nil
}

func flattenHeaders(h nethttp.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		out[k] = strings.Join(vs, ", ")
	}
	return out
}
