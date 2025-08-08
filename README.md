# Gateway MCP Proxy (MVP)

Multi-tenant MCP (Model Context Protocol) gateway that exposes enterprise REST APIs as MCP tools. Data plane in Go, control-plane APIs in the same binary for MVP, Postgres for persistence, and a mock upstream API for end-to-end testing.

## Repo structure
- `proxy/` Go MCP proxy + control plane
- `mock/` Go mock REST API (orders/products)
- `docker-compose.yml` One-VPS friendly setup (Postgres, Mock, Proxy)
- `proxy/openapi.yaml` Proxy API (for Postman)
- `mock/openapi.yaml` Mock API spec
- `PROGRESS.md` Running project notes

## Quick start (Docker Compose)
1) Build and start
```sh
docker compose build
docker compose up -d
```

2) Seed control plane (ADMIN_TOKEN defaults to `changeme`)
```sh
# Create tenant with egress allowlist
curl -X POST http://localhost:8080/api/tenants \
  -H 'Content-Type: application/json' -H 'X-Admin-Token: changeme' \
  -d '{
    "slug":"tenant-a",
    "name":"Tenant A",
    "enabled":true,
    "egressAllowlist":["mock","localhost","127.0.0.1"]
  }'

# Create server (slug is used in URL path)
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

# Add a tool mapping for the server
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

3) Test the MCP flow (JSON-RPC over Streamable HTTP)
```sh
# a) initialize → returns server info; header Mcp-Session-Id is set
curl -i -X POST http://localhost:8080/proxy/sales/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"mcp-inspector","version":"0.15.0"}}}'

# b) tools/list (include Mcp-Session-Id from initialize response headers)
curl -s -X POST http://localhost:8080/proxy/sales/mcp \
  -H 'Content-Type: application/json' \
  -H 'Mcp-Session-Id: <paste-session-id>' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'

# c) tools/call
curl -s -X POST http://localhost:8080/proxy/sales/mcp \
  -H 'Content-Type: application/json' \
  -H 'Mcp-Session-Id: <paste-session-id>' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"getOrder","arguments":{"orderId":"abc"}}}'
```

## Using MCP Inspector
- Server URL: `http://localhost:8080/proxy/sales/mcp`
- Transport: Streamable HTTP (JSON only)
- Authentication: With `UNPROTECTED=1` (default in compose), no JWT required. Otherwise configure Bearer token.

## Local dev (without Docker)
Mock API:
```sh
cd mock
go run ./cmd/mock
```
Proxy:
```sh
cd proxy
UNPROTECTED=1 GATEWAY_RESOURCE_AUDIENCE=http://localhost:8080/proxy go run ./cmd/proxy
```

## Environment variables (proxy)
- `UNPROTECTED` set to `1` to bypass JWT for MCP calls (dev only)
- `ADMIN_TOKEN` shared secret for control APIs (default: `changeme` in compose)
- `DATABASE_URL` Postgres DSN (compose sets it for you)
- `GATEWAY_RESOURCE_AUDIENCE` audience expected in JWTs (compose: `http://localhost:8080/proxy`)
- `MOCK_BASE_URL` default mock base (compose: `http://mock:9090`)

## Reset the database
Recreate schema (drops data):
```sh
docker compose down -v
docker compose build
docker compose up -d
# re-run the seeding curl commands above
```

## Troubleshooting
- 401 with `UNPROTECTED=1`: server/tenant missing in DB; seed via control plane; ensure URL uses an existing server slug (e.g., `sales`).
- MCP error `-32005 missing session`: include fresh `Mcp-Session-Id` header from `initialize`.
- MCP error `-32000 egress host not allowed`: add host (e.g., `mock`) to tenant `egressAllowlist` and re-POST the tenant.
- Inspector Zod error on `outputSchema.type`: only send `outputSchema` when it’s a valid JSON Schema object with `type: "object"`.
- Protected resource metadata is per-server: `GET /proxy/{server}/.well-known/oauth-protected-resource`.

## Specs
- Proxy API: see `proxy/openapi.yaml`
- Mock API: see `mock/openapi.yaml`
- MCP Tools spec (2025-06-18): https://modelcontextprotocol.io/specification/2025-06-18/server/tools
