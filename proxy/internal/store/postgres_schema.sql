-- Minimal schema for control plane persistence
create table if not exists tenants (
  id uuid primary key default gen_random_uuid(),
  slug text unique not null,
  name text not null,
  enabled boolean not null default true,
  egress_allowlist jsonb not null default '[]'::jsonb,
  created_at timestamptz not null default now()
);

create table if not exists servers (
  id uuid primary key default gen_random_uuid(),
  tenant_id uuid not null references tenants(id) on delete cascade,
  slug text unique not null,
  name text not null,
  audience text not null,
  enabled boolean not null default true,
  upstream_base_url text,
  server_title text,
  server_version text,
  instructions text,
  openapi_json jsonb,
  openapi_source_url text,
  updated_at timestamptz not null default now()
);

create table if not exists tools (
  id uuid primary key default gen_random_uuid(),
  server_id uuid not null references servers(id) on delete cascade,
  name text not null,
  title text,
  description text,
  required_scopes jsonb default '[]'::jsonb,
  input_schema jsonb,
  output_schema jsonb,
  enabled boolean not null default true,
  unique(server_id, name)
);

create table if not exists request_mappings (
  tool_id uuid primary key references tools(id) on delete cascade,
  method text not null,
  path text not null,
  query jsonb default '{}'::jsonb,
  headers jsonb default '{}'::jsonb,
  body jsonb default '{}'::jsonb
);

-- Convenience view to fetch tools with mapping and server slug
create or replace view tools_with_mappings as
select
  t.id,
  s.slug as server_slug,
  t.name,
  t.title,
  t.description,
  t.required_scopes,
  t.input_schema,
  t.output_schema,
  t.enabled,
  m.method,
  m.path,
  m.query,
  m.headers,
  m.body
from tools t
join request_mappings m on m.tool_id = t.id
join servers s on s.id = t.server_id;


