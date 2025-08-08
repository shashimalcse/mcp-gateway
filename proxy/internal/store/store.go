package store

// Store defines the minimal interface used by handlers so we can plug
// different backends (memory, postgres, etc.).
type Store interface {
	ResourceAudience() string

	GetTenant(slug string) (Tenant, error)
	GetServer(slug string) (Server, error)

	ListToolsByServer(serverSlug string) ([]Tool, error)
}
