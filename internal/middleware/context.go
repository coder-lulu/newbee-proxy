package middleware

import (
    "context"
    jwt "github.com/golang-jwt/jwt/v4"
)

// GetJWTClaims 从上下文中获取由中间件注入的 jwt.MapClaims
func GetJWTClaims(ctx context.Context) (jwt.MapClaims, bool) {
    if v := ctx.Value(claimsCtxKey); v != nil {
        if claims, ok := v.(jwt.MapClaims); ok {
            return claims, true
        }
    }
    return nil, false
}

