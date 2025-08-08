package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

type Session struct {
	ID           string
	ServerSlug   string
	TenantSlug   string
	CreatedAt    time.Time
	LastAccessed time.Time
	Claims       map[string]interface{}
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

func NewManager(ttl time.Duration) *Manager {
	return &Manager{sessions: make(map[string]*Session), ttl: ttl}
}

func (m *Manager) NewSession(serverSlug, tenantSlug string, claims map[string]interface{}) *Session {
	id := generateSessionID()
	s := &Session{ID: id, ServerSlug: serverSlug, TenantSlug: tenantSlug, CreatedAt: time.Now(), LastAccessed: time.Now(), Claims: claims}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	return s
}

func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return nil, errors.New("session not found")
	}
	if m.ttl > 0 && time.Since(s.LastAccessed) > m.ttl {
		m.Delete(id)
		return nil, errors.New("session expired")
	}
	m.mu.Lock()
	s.LastAccessed = time.Now()
	m.mu.Unlock()
	return s, nil
}

func (m *Manager) Delete(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

func generateSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
