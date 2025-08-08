package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

type PostgresStore struct {
	db               *sql.DB
	resourceAudience string
}

func NewPostgresStore(db *sql.DB, resourceAudience string) *PostgresStore {
	return &PostgresStore{db: db, resourceAudience: resourceAudience}
}

func (p *PostgresStore) ResourceAudience() string { return p.resourceAudience }

func (p *PostgresStore) GetTenant(slug string) (Tenant, error) {
	var t Tenant
	var allowJSON []byte
	row := p.db.QueryRowContext(context.Background(), `
        select slug, coalesce(name,''), coalesce(enabled,true), coalesce(egress_allowlist,'[]'::jsonb)
        from tenants where slug=$1
    `, slug)
	if err := row.Scan(&t.Slug, &t.Name, &t.Enabled, &allowJSON); err != nil {
		return Tenant{}, err
	}
	t.EgressAllowlist = []string{}
	_ = jsonUnmarshal(allowJSON, &t.EgressAllowlist)
	return t, nil
}

func (p *PostgresStore) GetServer(slug string) (Server, error) {
	var s Server
	row := p.db.QueryRowContext(context.Background(), `
        select s.slug,
               t.slug as tenant_slug,
               s.name,
               s.audience,
               s.enabled,
               s.upstream_base_url,
               coalesce(s.server_title,''),
               coalesce(s.server_version,''),
               coalesce(s.instructions,'')
        from servers s
        join tenants t on t.id = s.tenant_id
        where s.slug=$1
    `, slug)
	if err := row.Scan(&s.Slug, &s.TenantSlug, &s.Name, &s.Audience, &s.Enabled, &s.UpstreamBaseURL, &s.ServerTitle, &s.ServerVersion, &s.Instructions); err != nil {
		return Server{}, err
	}
	return s, nil
}

func (p *PostgresStore) ListToolsByServer(serverSlug string) ([]Tool, error) {
	rows, err := p.db.QueryContext(context.Background(), `
        select id, name, coalesce(title,''), coalesce(description,''), coalesce(required_scopes,'{}')::jsonb, coalesce(input_schema,'{}')::jsonb, coalesce(output_schema,'{}')::jsonb,
               method, path, coalesce(query,'{}')::jsonb, coalesce(headers,'{}')::jsonb, coalesce(body,'{}')::jsonb
        from tools_with_mappings
        where server_slug=$1 and enabled=true
        order by name
    `, serverSlug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tool{}
	for rows.Next() {
		var t Tool
		var scopesJSON, inJSON, outJSON, qJSON, hJSON, bJSON []byte
		if err := rows.Scan(&t.ID, &t.Name, &t.Title, &t.Description, &scopesJSON, &inJSON, &outJSON, &t.Mapping.Method, &t.Mapping.Path, &qJSON, &hJSON, &bJSON); err != nil {
			return nil, err
		}
		// Decode JSON columns into maps
		t.RequiredScopes = []string{}
		if len(scopesJSON) > 0 && string(scopesJSON) != "null" {
			// tolerate decoding failure silently for MVP
			_ = jsonUnmarshal(scopesJSON, &t.RequiredScopes)
		}
		t.InputSchema = map[string]interface{}{}
		_ = jsonUnmarshal(inJSON, &t.InputSchema)
		t.OutputSchema = map[string]interface{}{}
		_ = jsonUnmarshal(outJSON, &t.OutputSchema)
		t.Mapping.Query = map[string]string{}
		_ = jsonUnmarshal(qJSON, &t.Mapping.Query)
		t.Mapping.Headers = map[string]string{}
		_ = jsonUnmarshal(hJSON, &t.Mapping.Headers)
		t.Mapping.Body = map[string]interface{}{}
		_ = jsonUnmarshal(bJSON, &t.Mapping.Body)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// helpers
func jsonUnmarshal(b []byte, v interface{}) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return json.Unmarshal(b, v)
}

var _ Store = (*PostgresStore)(nil)

// --- Write methods for control plane ---

func (p *PostgresStore) UpsertTenant(t Tenant) error {
	allowJSON, _ := json.Marshal(t.EgressAllowlist)
	_, err := p.db.ExecContext(context.Background(), `
        insert into tenants (slug, name, enabled, egress_allowlist)
        values ($1,$2,$3,$4::jsonb)
        on conflict (slug) do update set name=excluded.name, enabled=excluded.enabled, egress_allowlist=excluded.egress_allowlist
    `, t.Slug, t.Name, t.Enabled, string(allowJSON))
	return err
}

func (p *PostgresStore) UpsertServer(s Server) error {
	_, err := p.db.ExecContext(context.Background(), `
        insert into servers (tenant_id, slug, name, audience, enabled, upstream_base_url, server_title, server_version, instructions)
        values ((select id from tenants where slug=$1), $2,$3,$4,$5,$6,$7,$8,$9)
        on conflict (slug) do update set
          name=excluded.name,
          audience=excluded.audience,
          enabled=excluded.enabled,
          upstream_base_url=excluded.upstream_base_url,
          server_title=excluded.server_title,
          server_version=excluded.server_version,
          instructions=excluded.instructions,
          updated_at=now()
    `, s.TenantSlug, s.Slug, s.Name, s.Audience, s.Enabled, s.UpstreamBaseURL, s.ServerTitle, s.ServerVersion, s.Instructions)
	return err
}

func (p *PostgresStore) UpdateServerOpenAPI(serverSlug string, specJSON []byte, sourceURL string) error {
	_, err := p.db.ExecContext(context.Background(), `
        update servers set openapi_json=$2, openapi_source_url=$3, updated_at=now() where slug=$1
    `, serverSlug, specJSON, sourceURL)
	return err
}

func (p *PostgresStore) UpsertToolsForServer(serverSlug string, tools []Tool) error {
	tx, err := p.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var serverID string
	if err := tx.QueryRowContext(context.Background(), `select id::text from servers where slug=$1`, serverSlug).Scan(&serverID); err != nil {
		return err
	}
	for _, t := range tools {
		var toolID string
		// upsert tool by (server_id, name)
		scopesJSON, _ := json.Marshal(t.RequiredScopes)
		inJSON, _ := json.Marshal(t.InputSchema)
		outJSON, _ := json.Marshal(t.OutputSchema)
		if err := tx.QueryRowContext(context.Background(), `
            insert into tools (server_id, name, title, description, required_scopes, input_schema, output_schema, enabled)
            values ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb,true)
            on conflict (server_id, name) do update set
              title=excluded.title,
              description=excluded.description,
              required_scopes=excluded.required_scopes,
              input_schema=excluded.input_schema,
              output_schema=excluded.output_schema,
              enabled=true
            returning id::text
        `, serverID, t.Name, t.Title, t.Description, string(scopesJSON), string(inJSON), string(outJSON)).Scan(&toolID); err != nil {
			return err
		}
		qJSON, _ := json.Marshal(t.Mapping.Query)
		hJSON, _ := json.Marshal(t.Mapping.Headers)
		bJSON, _ := json.Marshal(t.Mapping.Body)
		if _, err := tx.ExecContext(context.Background(), `
            insert into request_mappings (tool_id, method, path, query, headers, body)
            values ($1,$2,$3,$4::jsonb,$5::jsonb,$6::jsonb)
            on conflict (tool_id) do update set
              method=excluded.method,
              path=excluded.path,
              query=excluded.query,
              headers=excluded.headers,
              body=excluded.body
        `, toolID, t.Mapping.Method, t.Mapping.Path, string(qJSON), string(hJSON), string(bJSON)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
