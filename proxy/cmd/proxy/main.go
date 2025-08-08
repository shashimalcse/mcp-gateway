package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	_ "github.com/lib/pq"

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

	// Choose store backend: Postgres if DATABASE_URL is set, else in-memory
	var backend store.Store
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			log.Fatal(err)
		}
		if err := db.Ping(); err != nil {
			log.Fatal(err)
		}
		// bootstrap schema
		if schemaBytes, err := os.ReadFile("internal/store/postgres_schema.sql"); err == nil {
			if err := store.EnsureSchema(db, string(schemaBytes)); err != nil {
				log.Fatalf("schema init failed: %v", err)
			}
		} else {
			log.Printf("warning: could not read schema file: %v", err)
		}
		backend = store.NewPostgresStore(db, resourceAudience)
		log.Printf("Using Postgres store")
	} else {
		mem := store.NewMemoryStore(resourceAudience)
		seedDemo(mem)
		backend = mem
		log.Printf("Using in-memory store")
	}

	// JWT validator factory (per-tenant issuers)
	validator := auth.NewJWTValidator(backend)
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
	r.Get("/proxy/{server}/.well-known/oauth-protected-resource", handlers.ProtectedResourceMetadataHandler(backend))

	// Control plane APIs: protect with admin token if provided
	if pg, ok := backend.(*store.PostgresStore); ok {
		var cs handlers.ControlStore = pg
		adminToken := os.Getenv("ADMIN_TOKEN")
		mux := chi.NewRouter()
		mux.Post("/api/tenants", handlers.UpsertTenantHandler(cs))
		mux.Post("/api/servers", handlers.UpsertServerHandler(cs))
		mux.Post("/api/servers/{server}/openapi", handlers.UploadOpenAPIHandler(cs))
		mux.Post("/api/servers/{server}/tools", handlers.UpsertToolsHandler(cs))
		if adminToken != "" {
			r.Mount("/", auth.AdminTokenMiddleware(adminToken)(mux))
		} else {
			// If no token provided, leave open only when UNPROTECTED=1
			if config.Unprotected {
				r.Mount("/", mux)
			} else {
				log.Printf("control plane disabled: set ADMIN_TOKEN to enable in prod")
			}
		}
	}

	// Single MCP endpoint (POST JSON-RPC) and session DELETE per spec option
	r.With(auth.JWTAuthMiddleware(validator)).Post("/proxy/{server}/mcp", handlers.MCPEndpointHandler(backend, sessionManager))
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
	mockBase := getEnv("MOCK_BASE_URL", "http://localhost:9090")
	_ = s.UpsertServer(store.Server{
		Slug:            "sales",
		TenantSlug:      "tenant-a",
		Name:            "Sales API",
		Audience:        audience,
		AllowedIssuers:  nil, // use tenant-level issuers
		Enabled:         true,
		UpstreamBaseURL: mockBase,
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
		UpstreamBaseURL: mockBase,
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
