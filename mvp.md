## MVP Plan — AI Gateway (Go + Next.js, Single VPS)

### Scope and constraints (locked)
- **Stack**: Go (control plane API + MCP proxy/runtime), Next.js (dashboard/console).
- **MCP client**: MCP Inspector.
 - **MCP protocol**: Follow the latest MCP spec. Transport: **Streamable HTTP only** (no WebSocket, no stdio).
- **Tool invocation auth**: JWT only (RS256/ES256). Tenants configure their own authorization servers (issuer → JWKS). No opaque tokens, no DPoP/MTLS in MVP.
- **Dashboard/customer auth**: Clerk (hosted) for Next.js app authentication and session management.
- **Infra**: Single VPS deployment; Docker Compose; Let’s Encrypt via Caddy/NGINX; Postgres for metadata; local filesystem for spec storage (S3/minio optional later).
- **Cost posture**: Avoid cloud-native dependencies; keep ops light for a single-node footprint.

---

### High-level architecture (single VPS)
- **Reverse proxy (Caddy/NGINX)**: TLS termination + routing
  - `https://gateway.example.com/console` → Next.js (Clerk-protected)
  - `https://gateway.example.com/api` → Go Control Plane API
  - `https://gateway.example.com/proxy` → Go MCP Proxy/Runtime
- **Control Plane (Go)**: Tenant management, OpenAPI ingestion, tool generation, tool mapping CRUD, publish/revise tools.
- **MCP Proxy/Runtime (Go)**: MCP server endpoints, JWT validation, scope checks, execution of declarative mappings, upstream HTTP calls.
- **State**: Postgres (metadata); specs stored on local disk (`/var/lib/gateway/specs`) for MVP.

---

### MCP protocol compliance & transport
- Implement MCP server behavior per the latest MCP specification.
- Support only the MCP **Streamable HTTP** transport (compatible with MCP Inspector):
  - Use the MCP-defined streamable HTTP framing and envelopes with proper request/response correlation and keep-alives as required by the spec.
  - No WebSocket or stdio transports in MVP.

---

### Authentication and authorization model
- **Dashboard/Console (Next.js)**
  - Authentication via Clerk (email/password, OAuth providers as configured in Clerk dashboard).
  - Server-side session verification on API calls from the console to control plane.

- **MCP Tool Invocations (Proxy/Runtime)**
  - Accept only HTTP Bearer tokens with JWTs signed by tenant-configured issuers.
  - Validate via JWKS (cached); checks: `iss` in tenant allowlist, `aud` equals the server-level resource identifier, `exp`/`nbf` valid, and required `scope` present.
  - Policy model: simple scope checks per tool (no OPA in MVP).

- **Authorization Servers Configuration**
  - Per-tenant list of issuers (`issuer_url`) with optional explicit `jwks_uri` and allowed algorithms.
  - JWKS cached in-memory with short TTL (e.g., 5–10 minutes) and background refresh.

---

### Protected Resource Metadata (server-level, not per-tenant)
- Endpoint: `GET /proxy/.well-known/oauth-protected-resource`
- Represents a single MCP server resource identifier (audience) for the whole proxy instance.
- Lists all authorization servers configured across tenants (MVP simplification). Clients can discover issuer metadata.
- Example (shape only):

```json
{
  "resource": "https://gateway.example.com/proxy",
  "authorization_servers": [
    { "issuer": "https://idp.tenant-a.com", "metadata_url": "https://idp.tenant-a.com/.well-known/openid-configuration" },
    { "issuer": "https://idp.tenant-b.com", "metadata_url": "https://idp.tenant-b.com/.well-known/openid-configuration" }
  ],
  "token_formats_supported": ["jwt"],
  "scopes_supported": ["read:orders", "write:orders", "admin:tools"]
}
```

Notes:
- `aud` on tokens must equal the server resource (e.g., `https://gateway.example.com/proxy`).
- Tools still enforce per-tenant constraints and scopes at invocation time.

---

### Data model (MVP)
- `tenants`: id, name, slug, created_at, enabled
- `tenant_auth_servers`: id, tenant_id, issuer_url, metadata_url (nullable), jwks_uri (nullable), algorithms_allowed
- `openapi_specs`: id, tenant_id, version, storage_path, status, created_at
- `tools`: id, tenant_id, name, version, status, required_scopes[]
- `tool_mappings`: id, tool_id, version, request_template (JSON: method, path, query, headers, body mapping)

---

### Control Plane API (MVP endpoints)
- Tenants
  - `POST /api/tenants`
  - `PUT /api/tenants/{tenant}/auth-servers`
  - `PUT /api/tenants/{tenant}/status`
  - `PUT /api/tenants/{tenant}/audience` (sets server-level resource audience; informational in MVP)
- OpenAPI ingestion
  - `POST /api/tenants/{tenant}/openapi` (upload file or URL)
  - `POST /api/tenants/{tenant}/openapi/{version}/generate-tools`
- Tools
  - `GET /api/tenants/{tenant}/tools`
  - `PUT /api/tenants/{tenant}/tools/{tool}/mapping`
  - `PUT /api/tenants/{tenant}/tools/{tool}/publish`
- Diagnostics
  - `GET /api/jwks-cache` (basic cache stats)

All control plane routes are Clerk-protected via the Next.js app or service-to-service auth behind the reverse proxy.

---

### MCP Proxy/Runtime HTTP endpoints (Streamable HTTP transport)
- `GET /proxy/.well-known/oauth-protected-resource` (server-level metadata)
- `GET /proxy/{tenant}/mcp/tools` (list tools for a tenant) — response via MCP Streamable HTTP as needed
- `POST /proxy/{tenant}/mcp/invoke` (invoke tool: `{ toolId, args }`) — response via MCP Streamable HTTP

Invocation flow:
1. Extract Bearer JWT; validate (`iss`, signature via JWKS, `aud` == server resource, `exp`/`nbf`).
2. Check required scopes for the tool against token `scope` claim.
3. Resolve mapping to an HTTP request; enforce tenant egress allowlist.
4. Call upstream; return normalized response object.

---

### Declarative mapping (MVP DSL)
- Templating: `{{variable}}` with strict escaping for path/query/JSON.
- Built-ins: `formatDate`, `toUpper`, `base64`, `coalesce`.
- Structure:

```yaml
request:
  method: POST
  path: /customers/{{customerId}}/orders
  query:
    from: {{start | formatDate: "YYYY-MM-DD"}}
  headers:
    X-Tenant: {{tenant}}
  body:
    mode: {{summary ? "brief" : "full"}}
```

---

### Next.js dashboard (Clerk)
- Screens: Tenant list/detail, Authorization servers config, OpenAPI upload/fetch + validation, Tools list + mapping editor, Publish/unpublish, Test runner (paste JWT, run tool, see resolved request/response).
- Access control: Clerk orgs or roles to gate admin actions (MVP: admin vs viewer).

---

### Deployment (single VPS)
- Runtime: Docker Compose with services: `reverse-proxy`, `control-plane`, `proxy`, `nextjs`, `postgres`.
- TLS: Let’s Encrypt via Caddy/NGINX.
- Storage: Postgres volume; local directory for uploaded specs; nightly `pg_dump` backup.
- Sizing: 2–4 vCPU, 4–8 GB RAM.

---

### Acceptance criteria (with MCP Inspector)
- Server metadata available at `GET /proxy/.well-known/oauth-protected-resource` and lists the configured authorization servers.
- From MCP Inspector (Streamable HTTP transport), pointing to `https://gateway.example.com/proxy`:
  - List tools for a specific tenant via `/{tenant}/mcp/tools`.
  - Invoke a published tool end-to-end using a valid JWT (`aud` == server resource; `iss` in tenant allowlist; `scope` satisfied).
- Console login via Clerk works; admins can onboard tenant, add issuer, upload OpenAPI, generate and publish at least one tool.
— No WebSocket/stdio transport usage anywhere.

---

### Timeline (aggressive 3–4 weeks)
- Week 1: Control plane scaffolding, data model, OpenAPI ingestion (validate + store), JWKS validation wiring, Clerk auth in console.
- Week 2: Tool generation for basic GET/POST, minimal mapping DSL, proxy invocation flow, server-level RFC9728 endpoint.
- Week 3: Console UX for tenant/auth, upload/review/publish, test runner; end-to-end with MCP Inspector.
- Week 4: Hardening (rate limits per tenant, basic logs/metrics), Docker Compose, TLS, docs.

---

### Open items to confirm
- Domain for TLS and Clerk configuration (callback URLs).
- Whether to aggregate `scopes_supported` across all tenants in the server-level metadata or omit it initially.
- Any initial upstream APIs to validate the mappings against.


