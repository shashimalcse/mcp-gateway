package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"gateway/proxy/internal/auth"
	"gateway/proxy/internal/config"
	"gateway/proxy/internal/engine"
	"gateway/proxy/internal/session"
	"gateway/proxy/internal/store"
)

type ProtectedResourceMetadata struct {
	Resource              string                         `json:"resource"`
	AuthorizationServers  []store.AuthorizationServerRef `json:"authorization_servers"`
	TokenFormatsSupported []string                       `json:"token_formats_supported"`
	ScopesSupported       []string                       `json:"scopes_supported,omitempty"`
}

func ProtectedResourceMetadataHandler(s *store.MemoryStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverSlug := chi.URLParam(r, "server")
		srv, err := s.GetServer(serverSlug)
		if err != nil || !srv.Enabled {
			http.Error(w, "server not found or disabled", http.StatusNotFound)
			return
		}
		tenant, _ := s.GetTenant(srv.TenantSlug)
		refsMap := map[string]store.AuthorizationServerRef{}
		for _, iss := range tenant.AllowedIssuers {
			refsMap[iss] = store.AuthorizationServerRef{Issuer: iss, MetadataURL: iss + "/.well-known/openid-configuration"}
		}
		for _, iss := range srv.AllowedIssuers {
			refsMap[iss] = store.AuthorizationServerRef{Issuer: iss, MetadataURL: iss + "/.well-known/openid-configuration"}
		}
		refs := make([]store.AuthorizationServerRef, 0, len(refsMap))
		for _, v := range refsMap {
			refs = append(refs, v)
		}
		resp := ProtectedResourceMetadata{Resource: srv.Audience, AuthorizationServers: refs, TokenFormatsSupported: []string{"jwt"}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func ListToolsHandler(s *store.MemoryStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverSlug := chi.URLParam(r, "server")
		tools, err := s.ListToolsByServer(serverSlug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Tools []store.Tool `json:"tools"`
		}{Tools: tools})
	}
}

// Deprecated REST invoke handler removed in favor of JSON-RPC MCP endpoint.

// JSON-RPC minimal types
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}
type jsonRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCPEndpointHandler implements the single POST endpoint for Streamable HTTP (JSON only for MVP)
func MCPEndpointHandler(s *store.MemoryStore, sm *session.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Origin validation (if configured and not unprotected)
		if !config.Unprotected && len(config.AllowedOrigins) > 0 {
			origin := r.Header.Get("Origin")
			allowed := false
			for _, o := range config.AllowedOrigins {
				if o == origin {
					allowed = true
					break
				}
			}
			if !allowed {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
		}

		// Protocol version header check (tolerant)
		version := r.Header.Get("MCP-Protocol-Version")
		if version != "" && version != config.MCPProtocolVersionLatest && version != config.MCPProtocolVersionFallback {
			http.Error(w, "unsupported MCP protocol version", http.StatusBadRequest)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		serverSlug := chi.URLParam(r, "server")
		var rpcReq jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&rpcReq); err != nil {
			writeRPCError(w, rpcReq.ID, -32700, "parse error", nil)
			return
		}
		if rpcReq.JSONRPC != "2.0" {
			writeRPCError(w, rpcReq.ID, -32600, "invalid request", "jsonrpc must be 2.0")
			return
		}

		switch rpcReq.Method {
		case "initialize":
			// Parse initialize params
			var initParams struct {
				ProtocolVersion string                 `json:"protocolVersion"`
				Capabilities    map[string]interface{} `json:"capabilities"`
				ClientInfo      struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				} `json:"clientInfo"`
			}
			if len(rpcReq.Params) > 0 {
				if err := json.Unmarshal(rpcReq.Params, &initParams); err != nil {
					writeRPCError(w, rpcReq.ID, -32602, "invalid params", nil)
					return
				}
			}

			// Issue a new session and return session ID in header
			srv, err := s.GetServer(serverSlug)
			if err != nil {
				writeRPCError(w, rpcReq.ID, -32004, "server not found", nil)
				return
			}
			tenant, _ := s.GetTenant(srv.TenantSlug)
			var claims map[string]interface{}
			if c, ok := auth.ClaimsFromContext(r.Context()); ok {
				claims = c
			} else {
				claims = map[string]interface{}{}
			}
			sess := sm.NewSession(serverSlug, tenant.Slug, claims)
			w.Header().Set("Mcp-Session-Id", sess.ID)

			// Build InitializeResult with per-server info
			type ServerInfo struct {
				Name    string `json:"name"`
				Title   string `json:"title"`
				Version string `json:"version"`
			}
			result := map[string]interface{}{
				"protocolVersion": config.MCPProtocolVersionLatest,
				"capabilities": map[string]interface{}{
					"prompts":     map[string]interface{}{},
					"resources":   map[string]interface{}{"subscribe": true},
					"tools":       map[string]interface{}{},
					"logging":     map[string]interface{}{},
					"completions": map[string]interface{}{},
					"elicitation": map[string]interface{}{},
				},
				"serverInfo": ServerInfo{
					Name:    srv.Name,
					Title:   firstNonEmpty(srv.ServerTitle, srv.Name),
					Version: firstNonEmpty(srv.ServerVersion, "0.1.0"),
				},
				"instructions": firstNonEmpty(srv.Instructions, "Welcome to Gateway MCP Proxy."),
			}
			writeRPCResult(w, rpcReq.ID, result)
			return
		case "tools/list":
			if sid := r.Header.Get("Mcp-Session-Id"); sid == "" {
				writeRPCError(w, rpcReq.ID, -32005, "missing session", nil)
				return
			} else {
				if _, err := sm.Get(sid); err != nil {
					http.Error(w, "session not found", http.StatusNotFound)
					return
				}
			}
			tools, err := s.ListToolsByServer(serverSlug)
			if err != nil {
				writeRPCError(w, rpcReq.ID, -32004, "server not found", nil)
				return
			}
			// Map to MCP tool list shape per spec
			out := make([]map[string]interface{}, 0, len(tools))
			for _, t := range tools {
				tool := map[string]interface{}{
					"name":        t.Name,
					"description": t.Description,
					"inputSchema": t.InputSchema,
				}
				// Add optional fields if present
				if t.Title != "" {
					tool["title"] = t.Title
				}
				if t.OutputSchema != nil {
					tool["outputSchema"] = t.OutputSchema
				}
				out = append(out, tool)
			}
			result := map[string]interface{}{
				"tools": out,
			}
			writeRPCResult(w, rpcReq.ID, result)
			return
		case "tools/call":
			if sid := r.Header.Get("Mcp-Session-Id"); sid == "" {
				writeRPCError(w, rpcReq.ID, -32005, "missing session", nil)
				return
			} else {
				if _, err := sm.Get(sid); err != nil {
					http.Error(w, "session not found", http.StatusNotFound)
					return
				}
			}
			var params struct {
				ToolID string                 `json:"toolId"`
				Args   map[string]interface{} `json:"args"`
			}
			if err := json.Unmarshal(rpcReq.Params, &params); err != nil {
				writeRPCError(w, rpcReq.ID, -32602, "invalid params", nil)
				return
			}
			tool, ok := s.GetTool(serverSlug, params.ToolID)
			if !ok {
				writeRPCError(w, rpcReq.ID, -32001, "tool not found", nil)
				return
			}
			// Scope check (skip if unprotected)
			if !config.Unprotected {
				if claims, ok := auth.ClaimsFromContext(r.Context()); ok {
					if !hasRequiredScopes(claims, tool.RequiredScopes) {
						writeRPCError(w, rpcReq.ID, -32002, "insufficient_scope", nil)
						return
					}
				} else {
					writeRPCError(w, rpcReq.ID, -32003, "unauthorized", nil)
					return
				}
			}
			srv, _ := s.GetServer(serverSlug)
			tenant, _ := s.GetTenant(srv.TenantSlug)
			client := &http.Client{Timeout: 20 * time.Second}
			res, err := engine.Execute(r.Context(), client, srv, tenant, tool, params.Args)
			if err != nil {
				writeRPCError(w, rpcReq.ID, -32000, err.Error(), nil)
				return
			}
			writeRPCResult(w, rpcReq.ID, map[string]interface{}{"status": res.UpstreamStatus, "data": json.RawMessage(res.UpstreamBody)})
			return
			// removed duplicate initialize case
		case "terminate":
			if sid := r.Header.Get("Mcp-Session-Id"); sid != "" {
				sm.Delete(sid)
				writeRPCResult(w, rpcReq.ID, map[string]interface{}{"terminated": true})
				return
			}
			writeRPCError(w, rpcReq.ID, -32005, "missing session", nil)
			return
		default:
			writeRPCError(w, rpcReq.ID, -32601, "method not found", nil)
			return
		}
	}
}

func writeRPCResult(w http.ResponseWriter, id interface{}, result interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: result})
}
func writeRPCError(w http.ResponseWriter, id interface{}, code int, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: id, Error: &jsonRPCError{Code: code, Message: message, Data: data}})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// MCPSessionDeleteHandler handles HTTP DELETE to terminate a session
func MCPSessionDeleteHandler(sm *session.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverSlug := chi.URLParam(r, "server")
		sid := r.Header.Get("Mcp-Session-Id")
		if sid == "" {
			http.Error(w, "missing session", http.StatusBadRequest)
			return
		}
		s, err := sm.Get(sid)
		if err != nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		if s.ServerSlug != serverSlug {
			http.Error(w, "session does not belong to this server", http.StatusBadRequest)
			return
		}
		sm.Delete(sid)
		w.WriteHeader(http.StatusNoContent)
	}
}

func hasRequiredScopes(claims map[string]interface{}, required []string) bool {
	if len(required) == 0 {
		return true
	}
	// support space-separated scope string per OAuth 2.0
	var have map[string]bool = map[string]bool{}
	if s, ok := claims["scope"].(string); ok {
		for _, part := range splitSpaces(s) {
			have[part] = true
		}
	}
	// some IDPs may put scopes in an array claim
	if arr, ok := claims["scopes"].([]interface{}); ok {
		for _, v := range arr {
			if vs, ok := v.(string); ok {
				have[vs] = true
			}
		}
	}
	for _, need := range required {
		if !have[need] {
			return false
		}
	}
	return true
}

func splitSpaces(s string) []string {
	out := []string{}
	start := -1
	for i, c := range s {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		} else if start == -1 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}
