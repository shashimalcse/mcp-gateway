## Project: MCP Gateway (MVP) – Running Status

### Recent Updates
- Fixed Postgres query for servers: now joins `tenants` to fetch `tenant_slug` (removed reliance on non-existent `tenant_slug` column).
- Added `tenants.egress_allowlist jsonb` in schema; control plane (`POST /api/tenants`) now accepts `egressAllowlist`; `GetTenant` reads it; `UpsertTenant` persists it.
- MCP conformance:
  - `initialize` declares `tools: { listChanged: false }` capability.
  - `tools/list` accepts optional `cursor`, returns `{ result: { tools: [...] } }`, and emits JSON-RPC errors for missing/unknown session.
  - Omit `outputSchema` unless it is a valid JSON Schema object with `type: "object"` (fixes MCP Inspector Zod validation).
- Unprotected mode: `UNPROTECTED=1` bypasses JWT but still requires existing server/tenant. In Postgres mode, seed via control plane.
- Docker Compose: proxy includes `UNPROTECTED=1`, `MOCK_BASE_URL=http://mock:9090`; compose flow documented below.
- Mock service: added Chi logging middleware for request logs (`middleware.Logger`).

### Stack and Scope
- **Backend (proxy/data plane)**: Go
- **Console (control plane UI)**: Next.js (scaffold pending)
- **Auth for MCP tools**: JWT (tenants configure issuer URLs/JWKS); dev flag `UNPROTECTED=1` bypasses auth
- **MCP transport**: Streamable HTTP via single JSON-RPC 2.0 POST endpoint (no SSE/NDJSON for MVP)
- **Deploy**: Single VPS via Docker Compose (to be added)
- **Mock upstream API**: Go mock service for Orders/Products (used in local E2E)

### Current Folder Layout
- `proxy/`
  - `cmd/proxy/main.go`: Entry point (Chi router, routes, seeding demo servers/tools)
  - `internal/`
    - `handlers/handlers.go`: JSON-RPC `initialize`, `tools/list`, `tools/call`, plus `DELETE` for sessions; `.well-known` handler
    - `session/manager.go`: Session store; `Mcp-Session-Id` issuance/lookup/delete
    - `auth/jwt.go`, `auth/context.go`: JWT middleware (issuer/JWKS/audience); respects `UNPROTECTED`
    - `store/memory.go`, `store/helpers.go`: In-memory store (tenants, servers, tools)
    - `engine/engine.go`: Tool execution (render mapping, call upstream HTTP, return response)
    - `config/config.go`: Central config (MCP protocol version, dev flags, origins)
  - `openapi.yaml`: Up-to-date spec for Postman testing
- `mvp.md`, `proposal.md`: Product/architecture docs
- `mock/`
  - `cmd/mock/main.go`: Simple REST API (`/api/orders/{orderId}`, `/api/products/{productId}`)
  - `openapi.yaml`: OpenAPI 3.0 spec for the mock API

### Multi-Tenant Model (Servers per Customer)
- A customer (tenant) can have multiple MCP servers (e.g., `sales`, `products`).
- `store.Server` includes: `Slug`, `TenantSlug`, `Name`, `Audience`, `AllowedIssuers`, `Enabled`, `UpstreamBaseURL`, `ServerTitle`, `ServerVersion`, `Instructions`.
- Tools are stored per server: `ListToolsByServer(serverSlug)`.

### MCP Compliance (2025-06-18)
- Required header handling, stateful sessions, and method shapes implemented per spec.
- Implemented methods:
  - `initialize`: returns `protocolVersion`, `capabilities`, `serverInfo` (from server), and `instructions`. Also sets `Mcp-Session-Id` response header.
  - `tools/list`: requires `Mcp-Session-Id`. Returns `{ result: { tools: [...] } }` with each tool `{ name, title?, description, inputSchema, outputSchema? }`.
  - `tools/call`: requires `Mcp-Session-Id`. Executes via `engine.Execute` and returns content; scopes enforced. Accepts only spec params (`name`, `arguments`) — legacy shapes removed.
- Not implemented: optional SSE; tools pagination (`nextCursor`) can be added later.

### OpenAPI (Postman) – `proxy/openapi.yaml`
- Documents:
  - `GET /proxy/{server}/.well-known/oauth-protected-resource`
  - `POST /proxy/{server}/mcp` (JSON-RPC 2.0): examples for `initialize`, `tools/list`, `tools/call`
  - `DELETE /proxy/{server}/mcp`: terminates session (requires `Mcp-Session-Id`)
- Headers:
  - `MCP-Protocol-Version` (optional, recommended `2025-06-18`)
  - `Mcp-Session-Id` (required for non-initialize methods; returned by initialize)

### Seed Data (Demo)
- Servers:
  - `sales`: title "Sales MCP Server", version `0.1.0`, instructions set
  - `products`: title "Products MCP Server", version `0.1.0`, instructions set
- Tools:
  - `sales`: `getOrder` (inputSchema with `orderId`)
  - `products`: `getProduct` (inputSchema with `productId`)
- Upstream base URL (local dev): `http://localhost:9090`; tool paths use `/api/...`
- Egress allowlist: includes `localhost`, `127.0.0.1` (and `httpbin.org` retained)

### How to Run Locally
```sh
cd /Users/thilinashashimalsenarath/Desktop/Gateway/mock
go run ./cmd/mock

# in another terminal
cd /Users/thilinashashimalsenarath/Desktop/Gateway/proxy
UNPROTECTED=1 GATEWAY_RESOURCE_AUDIENCE=http://localhost:8080/proxy go run ./cmd/proxy
```

### Quick Test (cURL)
1) Initialize (get session ID in response headers):
```sh
curl -i -sS -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{"sampling":{},"roots":{"listChanged":true}},"clientInfo":{"name":"mcp-inspector","version":"0.15.0"}}}' \
  http://localhost:8080/proxy/sales/mcp
```
2) List tools:
```sh
curl -sS -H 'Content-Type: application/json' -H "Mcp-Session-Id: <paste-from-init>" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  http://localhost:8080/proxy/sales/mcp
```
3) Call a tool (hits mock upstream):
```sh
curl -sS -H 'Content-Type: application/json' -H "Mcp-Session-Id: <paste-from-init>" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"getOrder","arguments":{"orderId":"abc"}}}' \
  http://localhost:8080/proxy/sales/mcp
```

### Security & Auth
- JWT middleware validates `aud` against server audience and issuer via JWKS; can be bypassed with `UNPROTECTED=1` for dev/testing.
- Origin allowlist and strict `MCP-Protocol-Version` enforcement are planned to be strict in non-dev.

### Cleanups Completed
- Removed deprecated NDJSON streaming helper (`internal/transport/stream.go`).
- Fixed nested repo layout (`proxy/proxy` was flattened into `proxy`).
- Updated `openapi.yaml` to MCP-compliant request/response examples.
- Removed legacy `tools/call` param shape; now spec-only (name, arguments).
- Switched demo upstream from `httpbin.org` to local mock service; added localhost to egress allowlist.
 - Implemented Postgres schema for `egress_allowlist` and control-plane support.
 - `tools/list` output normalized to spec and Inspector’s Zod expectations.

### How to Run with Docker Compose
```sh
docker compose down -v
docker compose build
docker compose up -d

# Seed control plane (ADMIN_TOKEN defaults to changeme)
curl -X POST http://localhost:8080/api/tenants \
  -H 'Content-Type: application/json' -H 'X-Admin-Token: changeme' \
  -d '{
    "slug":"tenant-a",
    "name":"Tenant A",
    "enabled":true,
    "egressAllowlist":["mock","localhost","127.0.0.1"]
  }'

curl -X POST http://localhost:8080/api/servers \
  -H 'Content-Type: application/json' -H 'X-Admin-Token: changeme' \
  -d '{
    "slug":"sales",
    "tenantSlug":"tenant-a",
    "name":"Sales API",
    "audience":"http://localhost:8080/proxy",
    "enabled":true,
    "upstreamBaseURL":"http://mock:9090",
    "serverTitle":"Sales MCP Server",
    "serverVersion":"0.1.0",
    "instructions":"Use tools to interact with Sales API."
  }'

curl -X POST http://localhost:8080/api/servers/sales/tools \
  -H 'Content-Type: application/json' -H 'X-Admin-Token: changeme' \
  -d '{
    "tools":[{
      "name":"getOrder",
      "title":"Get Order",
      "description":"Retrieve order by id",
      "requiredScopes":[],
      "inputSchema":{
        "$schema":"http://json-schema.org/draft-07/schema#",
        "type":"object",
        "properties":{"orderId":{"type":"string","description":"Order ID"}},
        "required":["orderId"],
        "additionalProperties":false
      },
      "mapping":{"method":"GET","path":"/api/orders/{{orderId}}","headers":{"Accept":"application/json"}}
    }]
  }'
```

### Troubleshooting
- 401 with UNPROTECTED=1: DB likely empty; seed tenant/server/tools; URL server slug must exist.
- MCP -32000 egress host not allowed: add host to tenant `egressAllowlist` and re-POST tenant.
- Inspector Zod error on `outputSchema.type`: ensure `outputSchema.type == "object"` or omit.
- Missing session: include fresh `Mcp-Session-Id` from `initialize` in headers for `tools/*`.
- Protected resource metadata path: per-server at `/proxy/{server}/.well-known/oauth-protected-resource`.

### Known Follow-ups
- Enforce Origin allowlist and `MCP-Protocol-Version` in non-dev.
- Add pagination `nextCursor` for `tools/list`.
- Add Clerk-authenticated Next.js console scaffold.
- README with MCP Inspector setup and JSON-RPC examples.

### Reference
- MCP Tools spec (2025-06-18): https://modelcontextprotocol.io/specification/2025-06-18/server/tools


