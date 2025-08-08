package store

// AuthorizationServerRef mirrors handlers' ref to avoid import cycle
type AuthorizationServerRef struct {
	Issuer      string `json:"issuer"`
	MetadataURL string `json:"metadata_url,omitempty"`
}

// AllAuthorizationServerRefs returns a deduped list of issuers across tenants (best-effort)
func (s *MemoryStore) AllAuthorizationServerRefs() []AuthorizationServerRef {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]AuthorizationServerRef{}
	for _, t := range s.tenants {
		for _, iss := range t.AllowedIssuers {
			// best-effort metadata URL using OIDC discovery path
			if _, exists := seen[iss]; !exists {
				seen[iss] = AuthorizationServerRef{
					Issuer:      iss,
					MetadataURL: iss + "/.well-known/openid-configuration",
				}
			}
		}
	}
	// include per-server overrides as well
	for _, srv := range s.servers {
		for _, iss := range srv.AllowedIssuers {
			if _, exists := seen[iss]; !exists {
				seen[iss] = AuthorizationServerRef{
					Issuer:      iss,
					MetadataURL: iss + "/.well-known/openid-configuration",
				}
			}
		}
	}
	out := make([]AuthorizationServerRef, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	return out
}
