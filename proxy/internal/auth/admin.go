package auth

import (
	"net/http"
)

// AdminTokenMiddleware protects control-plane routes using a shared token.
// The client must send either:
// - Header: X-Admin-Token: <token>
// - or Authorization: Bearer <token>
func AdminTokenMiddleware(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("X-Admin-Token")
			if token == "" {
				if authz := r.Header.Get("Authorization"); len(authz) > 7 && authz[:7] == "Bearer " {
					token = authz[7:]
				}
			}
			if token == "" || token != expected {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
