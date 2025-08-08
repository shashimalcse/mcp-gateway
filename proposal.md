# AI Gateway SaaS — Architecture & Design (MCP Proxy)

This document describes a production-ready architecture for a multi-tenant **AI Gateway SaaS** that proxies enterprise REST APIs as MCP (Model Context Protocol) tools. It covers system goals, functional and non-functional requirements, component designs, data flow, security, deployment, and a suggested tech stack. It also explains how OpenAPI specs are ingested to automatically generate MCP tools and how users can author custom, argument-based transformation tools.

---

## Executive summary

You want a minimal UI SaaS that:

* Lets customers upload OpenAPI specs and auto-generates exact MCP tool definitions for each endpoint.
* Lets customers author *custom tools* that take a small set of arguments (form fields) and map/transform those into concrete API calls (payloads, path/query params, headers), then invoke the real API.
* Acts as an **Auth & Policy Gateway** that validates tokens (JWT/opaque/DPoP), enforces scopes and policies, and exposes MCP server endpoints per tenant.
* Exposes the OAuth2 Protected Resource Metadata endpoint (RFC9728) as required by MCP so clients and authorization servers can discover how to talk to your MCP resource.

This doc explains how to build that, secure it, scale it, and the trade-offs you'll hit.

---

# 1. Requirements

## 1.1 Functional

1. Multi-tenant ingestion of OpenAPI v2/v3 specs (upload + URL fetch).
2. Auto-generate MCP tool manifests from OpenAPI endpoints (input/output schemas, parameters, security requirements).
3. UI for creating and editing **custom tools** (map a few named args → an actual API invocation) with test harness.
4. Authentication/token validation for incoming MCP client calls; policy checks (scopes, role-based rules, attribute-based rules).
5. Per-tenant MCP server endpoint(s) and `.well-known` protected resource metadata (RFC9728).
6. Auditing, rate-limiting, observability, and developer logs (for customers to debug tool mappings).
7. Sandbox/safe execution for any customer-provided transformation logic.
8. Admin UI for tenant onboarding, keys, quotas, and policies.

## 1.2 Non-functional

* **Security-first**: must resist token theft, injection, SSRF, and prompt injection via tool outputs.
* **Low-latency**: extra gateway hop should add minimal overhead (target: <100ms added median).
* **Highly available**: 99.9% SLA target for control plane; 99.5% for proxy plane (depending on tier).
* **Scalable**: autoscale by tenant, by request volume, and by concurrency.
* **Cheap to operate for small tenants**: default tiers should be low-cost and pre-baked.

---

# 2. High-level architecture

```
+----------------+       +----------------------+       +-------------------+
|  Web Console   | <-->  | Control Plane (API)  | <-->  |  Metadata Store   |
|  (React/Tail)  |       | - Ingestion          |       | (Postgres/Redis)  |
+----------------+       | - Tool Generator     |       +-------------------+
      |                    | - Policy Manager     |               |
      v                    +----------------------+               v
+----------------+                                  +-----------------------------+
| MCP Proxy/Edge | ---- auth & policy ---->         | Upstream Enterprise APIs    |
| (per-tenant)   |                                  +-----------------------------+
| - Token validate|                                        
| - Tool runtime  |                                        
+----------------+
```

Two logical planes:

* **Control Plane** — ingestion, tool generation, policy and tenant management, UI. Stateful (Postgres), transactional.
* **Proxy/Data Plane** — per-tenant MCP server(s) that accept calls from MCP clients (LLM agents), validate tokens & policies, transform tool requests into API calls, call upstream services.

Separation lets you scale the expensive, latency-sensitive proxy horizontally and independently of the control plane.

---

# 3. Components (detailed)

## 3.1 Ingestion Service (OpenAPI uploader)

**Responsibilities**

* Accept file upload or URL to OpenAPI v2/v3 spec; validate syntax and security (no external references unless allowed).
* Normalize the spec: resolve `$ref`s (optionally inline remote refs), extract endpoints, methods, parameter schemas, and security requirements.
* Create canonical internal representation (IR) of each endpoint.
* Produce a proposed MCP tool manifest per endpoint.

**Key features**

* Safe remote fetch: fetch with egress restrictions (allow list), timeouts, strict header control.
* Spec linting and issue reporting on the UI.
* Versioning for API specs (so customers can roll back and keep MCP tools pinned to a version).

**Data stores**: Postgres (metadata), object store (S3) for uploaded spec files.

## 3.2 Tool Generator (OpenAPI → MCP tool manifest)

**Responsibilities**

* Convert an OpenAPI operation into an MCP tool definition (name, description, input schema, outputs, required auth/scopes).
* Detect and include required authentication semantics in the manifest (scopes, token formats).
* Optionally expose an example call template for each generated tool.

**Strategy**

* For each operation, create a minimal `input` schema that maps to path/query/body/header parameters. For complex payloads, generate a nested schema but provide a simplified surface (allow `rawPayload` option).
* Auto-generate example prompts for model authors demonstrating how to call the tool.

**Edge cases**

* Multipart / form-data endpoints -> expose a coherent schema but allow a raw override.
* Streams & websockets -> mark as unsupported for now, or provide an async job-based pattern.

## 3.3 Custom Tool Editor & Transformation Engine

**User goal**: Build tools that accept a few typed args (e.g., `customerId`, `dateRange`, `summary: boolean`) and map them to a real API call. The UI should be small, no-code friendly, but powerful.

**UI features**

* Form-based editor: name, description, typed inputs (string, number, boolean, enum, object), output schema.
* Drag-drop mapper: map input fields to path params, query params, headers, or JSON body fields.
* Field transforms: small built-in transforms (formatDate, toUpper, base64, JSONPath extract, regex replace).
* Test console: run the tool against a sandbox/test endpoint with sample args and view request/response.
* Preview of the resolved HTTP request template (path, headers, body).

**Transformation Engine options**

* **Declarative mapping** (recommended for safety): use a JSON/YAML mapping language with templating and standard transforms. Example:

  ```yaml
  request:
    method: POST
    path: /customers/{{customerId}}/orders
    query:
      from: {{startDate | formatDate: "YYYY-MM-DD"}}
      to: {{endDate | formatDate: "YYYY-MM-DD"}}
    body:
      mode: {{summary ? "brief" : "full"}}
  ```

* **Scripted transforms (advanced)**: allow sandboxed JavaScript / WASM functions for complex logic, executed in a strict runtime (time-limited, memory-limited, no network I/O). Use a hardened sandbox like WASM + WASI or a language VM with strict policies.

**Security**: Prefer declarative transforms by default. Only enable script mode for paying tiers, with review and resource limits.

## 3.4 Auth & Policy Gateway (Token validation and policy enforcement)

**Responsibilities**

* Validate tokens presented by MCP clients (Bearer, DPoP, MTLS-bound tokens). Support JWT verification (via JWKS), opaque token introspection (RFC7662), and resource indicators (RFC8707).
* Enforce scopes and policies at tool level (tool declares required scopes or claims).
* Implement rate-limits and quotas per-tenant and per-tool.
* Return proper WWW-Authenticate headers pointing to the protected resource metadata URL as required by RFC9728 when rejecting.

**Capabilities**

* JWKS caching and rotation handling.
* Token introspection with caching; graceful fallbacks.
* Enforcement hooks for attribute-based access control (ABAC): use token claims + contextual attributes (tenant config) to make allow/deny decisions.

**Policy language**

* Use a policy engine like Open Policy Agent (OPA) to express business rules. Policies reference token claims, tool metadata, and tenant attributes.

## 3.5 MCP Proxy / Tool Runtime

**Responsibilities**

* Expose MCP server endpoints expected by MCP clients (tools discovery, tool invocation endpoints, resource metadata endpoint).
* Accept MCP tool invocation, validate, authorize, then run the tool mapping to produce an HTTP request to upstream API.
* Call upstream API (with retries/circuit breakers), translate response into MCP tool output, and send back to client.
* Audit logs for each invocation (who, when, tool, resolved request, upstream response summary).

**Design notes**

* Runtime has a lightweight worker pool to execute transforms and orchestrate the HTTP call.
* Keep transform execution local to proxy node (no cross-node execution) for simplicity and debugging.
* Implement request templates with variable substitution and strict escaping rules to avoid injection.

## 3.6 Protected Resource Metadata Endpoint

Per RFC9728 and MCP spec, the MCP server must publish a protected resource metadata JSON document at a `.well-known` URL (default: `/.well-known/oauth-protected-resource` or per-resource path for multi-tenancy).

**Contents** should include:

* `resource` (resource identifier / audience)
* `authorization_servers` (list of auth servers with their metadata URLs)
* `scopes_supported`
* `token_endpoint_auth_methods_supported` (e.g., `private_key_jwt`, `client_secret_basic`)
* `token_formats_supported` (e.g., `jwt`, `opaque`)
* `jwks_uri` (optional) if tokens are JWTs your server accepts locally
* flags for `dpop`, `tls_client_certificate_bound_access_tokens`, etc.

This endpoint must be discoverable and linked in `WWW-Authenticate` when issuing a 401.

## 3.7 Observability & Auditing

* Tracing: OpenTelemetry (propagate trace across proxy -> upstream calls).
* Metrics: Prometheus counters/gauges for requests, latencies, errors per-tenant/per-tool.
* Logs: Structured logs with request id, tenant id, tool id; redaction for PII.
* Auditing: append-only event store for tool invocations, policy decisions, and admin actions (immutable retention for compliance).

## 3.8 Admin & Developer UX

* Tenant onboarding flow: create tenant, configure trusted auth servers (issuer URLs / JWKS), upload OpenAPI, review generated tools.
* Tools gallery: list, search, enable/disable tools; version pinning; rollback.
* Policy editor: basic UI for scope-to-tool mapping and OPA policy upload.
* Test harness: interactive console to run a tool with sample args, view resolved HTTP request and upstream response.

---

# 4. Data flows (typical scenarios)

## 4.1 Upload OpenAPI -> Generate tools

1. Tenant uploads OpenAPI file to control plane.
2. Ingestion service validates and normalizes spec; stores file in S3 and meta in Postgres.
3. Tool generator emits one or more MCP tool manifests and example templates.
4. Tenant reviews and publishes tools; control plane pushes tool manifests/config to proxy nodes (via config service or pub/sub).

## 4.2 MCP client invokes tool (runtime)

1. Client (LLM agent) queries MCP server for tool list or directly invokes a tool.
2. Proxy receives request, extracts bearer token and resource indicator.
3. Gateway validates token (JWKS or introspection) and checks token audience / resource.
4. Policy engine determines if the token has scope/claims for this tool.
5. If allowed, proxy resolves the tool mapping with provided args to an HTTP request template.
6. Transform engine executes mapping (decl or sandboxed script).
7. Proxy issues HTTP request to upstream API, with configured connection pool & circuit breaker.
8. Upstream responds; proxy optionally normalizes response and returns the MCP tool result to the client.
9. Proxy emits audit event and metrics.

---

# 5. Security considerations

This is not exhaustive but covers the big risks.

## 5.1 Token & auth

* Validate audience/resource on tokens. Require `resource` param for token acquisition (RFC8707) to scope tokens.
* Prefer JWTs with robust signing (RS256/ES256). Support introspection for opaque tokens.
* Support DPoP and MTLS if tenants require proof-of-possession tokens.

## 5.2 Sandboxing transform logic

* Declarative mapping is safest. For scripted transforms:

  * Use Wasm sandboxes or JS isolates (like QuickJS in a sandbox) with strict resource/time limits.
  * Disable outbound network, file, and OS access in sandbox.
  * Limit memory and CPU and put an execution timeout (e.g., 200ms) per invocation.

## 5.3 Prevent SS R F & Host header attacks

* Upstream calls should only be allowed to configured host allow-lists or the target host derived from the tenant’s config. Deny arbitrary external hostnames unless explicitly whitelisted.
* Normalize and validate HTTP redirects from upstream.

## 5.4 Protect against injection & prompt attacks

* Escape template substitutions safely for path, query, JSON body, and headers.
* For textual fields that may be used in prompts, mark them as untrusted and do not allow them to influence tool selection or templates unless explicitly sanitized.

## 5.5 Auditing & retention

* Immutable audit log for compliance: include who invoked which tool, token identifier (hashed), resolved request (redact secrets), and response status code.
* Retention policy configurable per-tenant.

---

# 6. Multi-tenancy & isolation

* Tenant metadata and tool manifests in Postgres; per-tenant config stored encrypted.
* Proxy runs either shared multi-tenant processes (with strong logical separation) or dedicated per-tenant containers for higher isolation (premium tier).
* Use network policies and secrets per-tenant. Keep secrets encrypted at rest (KMS/HSM) and in transit.

---

# 7. Deployment & scaling

**Kubernetes-based** deployment recommended.

### Control plane

* Small stateful services (Postgres, Redis, object store) behind HA proxies.
* Ingestion workers horizontally scalable.

### Proxy / Data plane

* Stateless MCP proxy instances behind a global LB (per region).
* Use an API gateway (e.g., Envoy) in front of proxies for connection handling, TLS, and advanced routing.
* Horizontal autoscaling based on CPU and request queue length.
* For high-security tenants, provide private tenancy options (VPC-peering or private SaaS instances).

**Caching**: cache JWKS and introspection responses short-term; cache tool manifests locally on proxy nodes for fast lookups.

---

# 8. Observability, SLOs, & billing

* Track request latency P50/P95/P99 per-tenant and per-tool.
* Track policy denies, failed transformations, and upstream error rates.
* Expose billing metrics: number of tool calls, egress bandwidth, transform script runtime, storage.

---

# 9. Example MCP protected resource metadata (RFC9728) JSON

```json
{
  "resource": "https://mcp.example.com/tenants/abc",
  "authorization_servers": [
    {
      "issuer": "https://idp.example.com",
      "metadata_url": "https://idp.example.com/.well-known/openid-configuration"
    }
  ],
  "scopes_supported": ["read:orders","write:orders","admin:tools"],
  "token_formats_supported": ["jwt","opaque"],
  "jwks_uri": "https://mcp.example.com/tenants/abc/jwks.json",
  "dpop_bound_access_tokens_required": false,
  "tls_client_certificate_bound_access_tokens": false
}
```

Place this JSON at `https://{resource-host}/.well-known/oauth-protected-resource` (or per-resource path for multi-tenant hosts).

---

# 10. Tech stack & libraries (suggested)

* **Language**: Go (for proxy/data-plane performance) + TypeScript/React for Control Plane UI; NodeJS is an acceptable alternative.
* **Proxy**: Custom Go proxy with Envoy in front or a gRPC-based control plane; consider using NGINX/Envoy for TLS & L7 features.
* **Policy**: Open Policy Agent (OPA) for ABAC.
* **Sandbox**: WASM/WASI runtime (e.g., Wasmtime) or quickjs isolate with constraints.
* **DB**: Postgres for metadata, Redis for caches, S3-compatible store for spec files.
* **Observability**: OpenTelemetry + Prometheus + Grafana.
* **Secrets**: KMS (AWS KMS / GCP KMS / HashiCorp Vault).

---

# 11. Roadmap & MVP

**MVP (4–8 weeks)**

* Upload OpenAPI and generate MCP tool manifests for basic GET/POST endpoints.
* Minimal MCP proxy that accepts token-bearing requests, validates JWT via JWKS, maps a generated tool to an upstream API call, and returns results.
* Expose `.well-known/oauth-protected-resource` with tenant metadata.
* Small UI to upload spec, review generated tools, and test-run tools.

**Phase 2**

* Add declarative custom tool editor + mapping transforms.
* Add opaque token introspection and JWKS caching.
* Add auditing, metrics, and OPA-based policies.

**Phase 3**

* Scripted transforms with sandbox (WASM).
* Multi-region deployment, premium private tenancy.
* Billing, quotas, developer portal & SDKs.

---

# 12. Implementation pitfalls & tradeoffs

* **Scripted transforms vs declarative**: scripted gives power at the cost of attack surface and operational complexity. Start with declarative and gate scripting.
* **Per-tenant vs shared proxy**: dedicated per-tenant instances cost more but simplify isolation and compliance. Offer both.
* **Token validation**: introspection adds external latency; caching must be tuned to avoid stale revocations.

---

# 13. Appendix: Sample tool manifest (generated from OpenAPI)

```json
{
  "id": "getOrder",
  "name": "Get Order",
  "description": "Retrieve order by id",
  "input_schema": {
    "type": "object",
    "properties": {
      "orderId": {"type": "string"}
    },
    "required": ["orderId"]
  },
  "auth": {
    "scopes": ["read:orders"],
    "resource": "https://mcp.example.com/tenants/abc"
  },
  "request_template": {
    "method": "GET",
    "path": "/orders/{{orderId}}",
    "headers": {"Accept": "application/json"}
  },
  "response_schema": {"type":"object"}
}
```

---

# 14. Next steps (implementation artifacts I can produce)

* Component-level API contracts (OpenAPI) for the control plane and proxy.
* A small PoC repo (Go proxy + TypeScript control plane stub) demonstrating OpenAPI ingestion, manifest generation, and a running MCP endpoint.
* A step-by-step Kubernetes helm chart for the MVP stack.

Pick one and I’ll produce the next deliverable.

---

*End of document.*
