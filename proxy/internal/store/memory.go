package store

import (
	"errors"
	"sync"
)

type Tenant struct {
	Slug             string
	Name             string
	AllowedIssuers   []string
	EgressAllowlist  []string
	Enabled          bool
	CreatedUnixMilli int64
}

type Server struct {
	Slug       string
	TenantSlug string
	Name       string
	Audience   string
	// Optional override; if empty use tenant AllowedIssuers
	AllowedIssuers  []string
	Enabled         bool
	UpstreamBaseURL string
	ServerTitle     string
	ServerVersion   string
	Instructions    string
}

type Tool struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Title          string                 `json:"title,omitempty"`
	Description    string                 `json:"description"`
	RequiredScopes []string               `json:"requiredScopes"`
	Mapping        RequestTemplate        `json:"mapping"`
	InputSchema    map[string]interface{} `json:"inputSchema,omitempty"`
	OutputSchema   map[string]interface{} `json:"outputSchema,omitempty"`
}

type RequestTemplate struct {
	Method  string                 `json:"method"`
	Path    string                 `json:"path"`
	Query   map[string]string      `json:"query,omitempty"`
	Headers map[string]string      `json:"headers,omitempty"`
	Body    map[string]interface{} `json:"body,omitempty"`
}

type MemoryStore struct {
	mu               sync.RWMutex
	resourceAudience string
	tenants          map[string]Tenant
	servers          map[string]Server
	toolsByServer    map[string][]Tool
}

func NewMemoryStore(resourceAudience string) *MemoryStore {
	return &MemoryStore{
		resourceAudience: resourceAudience,
		tenants:          make(map[string]Tenant),
		servers:          make(map[string]Server),
		toolsByServer:    make(map[string][]Tool),
	}
}

func (s *MemoryStore) ResourceAudience() string { return s.resourceAudience }

func (s *MemoryStore) UpsertTenant(t Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenants[t.Slug] = t
	return nil
}

func (s *MemoryStore) GetTenant(slug string) (Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[slug]
	if !ok {
		return Tenant{}, errors.New("tenant not found")
	}
	return t, nil
}

func (s *MemoryStore) UpsertServer(srv Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.servers[srv.Slug] = srv
	return nil
}

func (s *MemoryStore) GetServer(slug string) (Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	srv, ok := s.servers[slug]
	if !ok {
		return Server{}, errors.New("server not found")
	}
	return srv, nil
}

func (s *MemoryStore) UpsertToolsForServer(serverSlug string, tools []Tool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolsByServer[serverSlug] = tools
	return nil
}

func (s *MemoryStore) ListToolsByServer(serverSlug string) ([]Tool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tools, ok := s.toolsByServer[serverSlug]
	if !ok {
		return []Tool{}, nil
	}
	return tools, nil
}

func (s *MemoryStore) GetTool(serverSlug, toolID string) (Tool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tools, ok := s.toolsByServer[serverSlug]
	if !ok {
		return Tool{}, false
	}
	for _, t := range tools {
		if t.ID == toolID {
			return t, true
		}
	}
	return Tool{}, false
}
