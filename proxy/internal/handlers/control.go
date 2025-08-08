package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"gateway/proxy/internal/store"

	"github.com/go-chi/chi/v5"
)

type ControlStore interface {
	UpsertTenant(store.Tenant) error
	UpsertServer(store.Server) error
	UpdateServerOpenAPI(serverSlug string, specJSON []byte, sourceURL string) error
	UpsertToolsForServer(serverSlug string, tools []store.Tool) error
}

func UpsertTenantHandler(s ControlStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var t store.Tenant
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if t.Slug == "" || t.Name == "" {
			http.Error(w, "slug and name required", http.StatusBadRequest)
			return
		}
		// If egress allowlist is omitted, default to empty list
		if t.EgressAllowlist == nil {
			t.EgressAllowlist = []string{}
		}
		if err := s.UpsertTenant(t); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func UpsertServerHandler(s ControlStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var srv store.Server
		if err := json.NewDecoder(r.Body).Decode(&srv); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if srv.Slug == "" || srv.TenantSlug == "" || srv.Name == "" || srv.Audience == "" {
			http.Error(w, "slug, tenantSlug, name, audience required", http.StatusBadRequest)
			return
		}
		if err := s.UpsertServer(srv); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func UploadOpenAPIHandler(s ControlStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverSlug := chi.URLParam(r, "server")
		sourceURL := r.URL.Query().Get("sourceUrl")
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) == 0 {
			http.Error(w, "missing body", http.StatusBadRequest)
			return
		}
		// Accept as JSON only for MVP
		var js map[string]interface{}
		if err := json.Unmarshal(body, &js); err != nil {
			http.Error(w, "invalid openapi json", http.StatusBadRequest)
			return
		}
		normalized, _ := json.Marshal(js)
		if err := s.UpdateServerOpenAPI(serverSlug, normalized, sourceURL); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func UpsertToolsHandler(s ControlStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverSlug := chi.URLParam(r, "server")
		var payload struct {
			Tools []store.Tool `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if len(payload.Tools) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err := s.UpsertToolsForServer(serverSlug, payload.Tools); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
