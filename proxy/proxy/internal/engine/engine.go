package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"gateway/proxy/internal/store"
)

type ExecuteResult struct {
	UpstreamStatus  int
	UpstreamBody    json.RawMessage
	UpstreamHeaders http.Header
}

func Execute(ctx context.Context, httpClient *http.Client, srv store.Server, tenant store.Tenant, tool store.Tool, args map[string]interface{}) (*ExecuteResult, error) {
	if srv.UpstreamBaseURL == "" {
		return nil, errors.New("upstream base URL not configured")
	}
	// Egress allowlist
	u, err := url.Parse(srv.UpstreamBaseURL)
	if err != nil {
		return nil, err
	}
	if !isHostAllowed(u.Hostname(), tenant.EgressAllowlist) {
		return nil, fmt.Errorf("egress host not allowed: %s", u.Hostname())
	}

	// Build request URL
	path := substitute(tool.Mapping.Path, args)
	base := strings.TrimRight(srv.UpstreamBaseURL, "/")
	full := base + path
	reqURL, err := url.Parse(full)
	if err != nil {
		return nil, err
	}
	q := reqURL.Query()
	for k, v := range tool.Mapping.Query {
		q.Set(k, substitute(v, args))
	}
	reqURL.RawQuery = q.Encode()

	// Body
	var body io.Reader
	if tool.Mapping.Body != nil {
		// simple arg substitution for string fields inside body
		resolved := resolveBody(tool.Mapping.Body, args)
		buf, _ := json.Marshal(resolved)
		body = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(tool.Mapping.Method), reqURL.String(), body)
	if err != nil {
		return nil, err
	}
	// Headers
	hasContentType := false
	for k, v := range tool.Mapping.Headers {
		sv := substitute(v, args)
		req.Header.Set(k, sv)
		if strings.ToLower(k) == "content-type" {
			hasContentType = true
		}
	}
	if body != nil && !hasContentType {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	// Try to keep as JSON; if not JSON, wrap as string
	var raw json.RawMessage
	if json.Valid(respBody) {
		raw = json.RawMessage(respBody)
	} else {
		// wrap into {"text": "..."}
		wrapped, _ := json.Marshal(map[string]string{"text": string(respBody)})
		raw = json.RawMessage(wrapped)
	}
	return &ExecuteResult{UpstreamStatus: resp.StatusCode, UpstreamBody: raw, UpstreamHeaders: resp.Header}, nil
}

func isHostAllowed(host string, allowlist []string) bool {
	for _, a := range allowlist {
		if strings.EqualFold(host, a) {
			return true
		}
	}
	return false
}

func substitute(template string, args map[string]interface{}) string {
	out := template
	for k, v := range args {
		placeholder := "{{" + k + "}}"
		val := fmt.Sprintf("%v", v)
		out = strings.ReplaceAll(out, placeholder, val)
	}
	return out
}

func resolveBody(body map[string]interface{}, args map[string]interface{}) map[string]interface{} {
	resolved := make(map[string]interface{}, len(body))
	for k, v := range body {
		switch t := v.(type) {
		case string:
			resolved[k] = substitute(t, args)
		case map[string]interface{}:
			resolved[k] = resolveBody(t, args)
		default:
			resolved[k] = v
		}
	}
	return resolved
}
