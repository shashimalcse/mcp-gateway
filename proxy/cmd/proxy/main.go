package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"gateway/proxy/internal/auth"
	"gateway/proxy/internal/config"
	"gateway/proxy/internal/handlers"
	"gateway/proxy/internal/session"
	"gateway/proxy/internal/store"
)

func main() {
	// Configuration (MVP: simple env + in-memory)
	resourceAudience := getEnv("GATEWAY_RESOURCE_AUDIENCE", "https://gateway.local/proxy")
	httpAddr := getEnv("HTTP_ADDR", ":8080")

	// In-memory store seeded with a demo tenant, server and tools
	memoryStore := store.NewMemoryStore(resourceAudience)
	seedDemo(memoryStore)

	// JWT validator factory (per-tenant issuers)
	validator := auth.NewJWTValidator(memoryStore)
	if os.Getenv("UNPROTECTED") == "1" || os.Getenv("UNPROTECTED") == "true" {
		config.Unprotected = true
	}

	// Session manager (e.g., 30 minutes idle TTL)
	sessionManager := session.NewManager(30 * time.Minute)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Server-level protected resource metadata (RFC9728)
	r.Get("/proxy/{server}/.well-known/oauth-protected-resource", handlers.ProtectedResourceMetadataHandler(memoryStore))

	// Single MCP endpoint (POST JSON-RPC) and session DELETE per spec option
	r.With(auth.JWTAuthMiddleware(validator)).Post("/proxy/{server}/mcp", handlers.MCPEndpointHandler(memoryStore, sessionManager))
	r.With(auth.JWTAuthMiddleware(validator)).Delete("/proxy/{server}/mcp", handlers.MCPSessionDeleteHandler(sessionManager))

	log.Printf("MCP proxy listening on %s (audience=%s)", httpAddr, resourceAudience)
	if err := http.ListenAndServe(httpAddr, r); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func seedDemo(s *store.MemoryStore) {
	// Demo tenant
	_ = s.UpsertTenant(store.Tenant{
		Slug:             "tenant-a",
		Name:             "Tenant A",
		AllowedIssuers:   []string{"https://issuer.example.com"},
		EgressAllowlist:  []string{"httpbin.org", "localhost", "127.0.0.1"},
		Enabled:          true,
		CreatedUnixMilli: time.Now().UnixMilli(),
	})

	// Two servers for the tenant: sales and products
	audience := s.ResourceAudience()
	_ = s.UpsertServer(store.Server{
		Slug:            "sales",
		TenantSlug:      "tenant-a",
		Name:            "Sales API",
		Audience:        audience,
		AllowedIssuers:  nil, // use tenant-level issuers
		Enabled:         true,
		UpstreamBaseURL: "http://localhost:9090",
		ServerTitle:     "Sales MCP Server",
		ServerVersion:   "0.1.0",
		Instructions:    "Use tools to interact with Sales API.",
	})
	_ = s.UpsertServer(store.Server{
		Slug:            "products",
		TenantSlug:      "tenant-a",
		Name:            "Products API",
		Audience:        audience,
		AllowedIssuers:  nil,
		Enabled:         true,
		UpstreamBaseURL: "http://localhost:9090",
		ServerTitle:     "Products MCP Server",
		ServerVersion:   "0.1.0",
		Instructions:    "Use tools to interact with Products API.",
	})

	_ = s.UpsertToolsForServer("sales", []store.Tool{
		{ID: "getOrder", Name: "getOrder", Title: "Get Order", Description: "Retrieve order by id", RequiredScopes: []string{"read:orders"}, Mapping: store.RequestTemplate{Method: "GET", Path: "/api/orders/{{orderId}}", Headers: map[string]string{"Accept": "application/json"}}, InputSchema: map[string]interface{}{"$schema": "http://json-schema.org/draft-07/schema#", "type": "object", "properties": map[string]interface{}{"orderId": map[string]interface{}{"type": "string", "description": "Order ID"}}, "required": []string{"orderId"}, "additionalProperties": false}},
	})
	_ = s.UpsertToolsForServer("products", []store.Tool{
		{ID: "getProduct", Name: "getProduct", Title: "Get Product", Description: "Retrieve product by id", RequiredScopes: []string{"read:products"}, Mapping: store.RequestTemplate{Method: "GET", Path: "/api/products/{{productId}}", Headers: map[string]string{"Accept": "application/json"}}, InputSchema: map[string]interface{}{"$schema": "http://json-schema.org/draft-07/schema#", "type": "object", "properties": map[string]interface{}{"productId": map[string]interface{}{"type": "string", "description": "Product ID"}}, "required": []string{"productId"}, "additionalProperties": false}},
	})
}
