package auth

import (
	"context"

	"github.com/golang-jwt/jwt/v4"
)

type contextKey string

const claimsKey contextKey = "jwtClaims"

func WithClaims(ctx context.Context, claims jwt.MapClaims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

func ClaimsFromContext(ctx context.Context) (jwt.MapClaims, bool) {
	v := ctx.Value(claimsKey)
	c, ok := v.(jwt.MapClaims)
	return c, ok
}
