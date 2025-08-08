package auth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	keyfunc "github.com/MicahParks/keyfunc"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v4"

	"gateway/proxy/internal/config"
	"gateway/proxy/internal/store"
)

type JWTValidator struct {
	store store.Store
	// MVP: simple per-issuer JWKS cache
	cache map[string]*keyfunc.JWKS
}

func NewJWTValidator(s store.Store) *JWTValidator {
	return &JWTValidator{store: s, cache: make(map[string]*keyfunc.JWKS)}
}

func (v *JWTValidator) getJWKS(jwksURI string) (*keyfunc.JWKS, error) {
	if jwks, ok := v.cache[jwksURI]; ok {
		return jwks, nil
	}
	jwks, err := keyfunc.Get(jwksURI, keyfunc.Options{RefreshErrorHandler: func(err error) {
		// noop for MVP
	}, RefreshInterval: time.Minute * 5})
	if err != nil {
		return nil, err
	}
	v.cache[jwksURI] = jwks
	return jwks, nil
}

func JWTAuthMiddleware(validator *JWTValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serverSlug := chi.URLParam(r, "server")
			srv, err := validator.store.GetServer(serverSlug)
			if err != nil || !srv.Enabled {
				http.Error(w, "server not found or disabled", http.StatusUnauthorized)
				return
			}
			tenant, err := validator.store.GetTenant(srv.TenantSlug)
			if err != nil || !tenant.Enabled {
				http.Error(w, "tenant not found or disabled", http.StatusUnauthorized)
				return
			}

			if config.Unprotected {
				// Skip auth entirely in unprotected mode
				next.ServeHTTP(w, r)
				return
			}

			authz := r.Header.Get("Authorization")
			if !strings.HasPrefix(authz, "Bearer ") {
				unauthorizedWithWWWAuthenticate(w)
				return
			}
			tokenString := strings.TrimPrefix(authz, "Bearer ")

			// MVP: assume issuer has well-known JWKS endpoint
			// In a real system we'd store jwks_uri per issuer; here we try "/.well-known/jwks.json"
			var claims jwt.MapClaims
			issuers := tenant.AllowedIssuers
			if len(srv.AllowedIssuers) > 0 {
				issuers = srv.AllowedIssuers
			}
			for _, issuer := range issuers {
				jwksURI := fmt.Sprintf("%s/.well-known/jwks.json", strings.TrimSuffix(issuer, "/"))
				jwks, err := validator.getJWKS(jwksURI)
				if err != nil {
					continue
				}
				token, err := jwt.ParseWithClaims(tokenString, jwt.MapClaims{}, jwks.Keyfunc)
				if err != nil {
					continue
				}
				if !token.Valid {
					continue
				}
				if c, ok := token.Claims.(jwt.MapClaims); ok {
					// check issuer and audience
					if c["iss"] != issuer {
						continue
					}
					switch aud := c["aud"].(type) {
					case string:
						if aud != srv.Audience {
							continue
						}
					case []interface{}:
						matched := false
						for _, a := range aud {
							if as, ok := a.(string); ok && as == srv.Audience {
								matched = true
								break
							}
						}
						if !matched {
							continue
						}
					default:
						continue
					}
					claims = c
				}
				break
			}
			if claims == nil {
				unauthorizedWithWWWAuthenticate(w)
				return
			}

			// Attach claims to context for handlers
			ctx := WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func unauthorizedWithWWWAuthenticate(w http.ResponseWriter) {
	// Minimal WWW-Authenticate header referencing protected resource metadata
	w.Header().Set("WWW-Authenticate", "Bearer realm=\"MCP Proxy\", error=\"invalid_token\"")
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
