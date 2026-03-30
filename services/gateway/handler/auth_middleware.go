package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type AccessClaims struct {
	UserID string `json:"uid"`
	Type   string `json:"typ"`
	jwt.RegisteredClaims
}

var jwtAccessSecret string

func SetJWTAccessSecret(secret string) {
	jwtAccessSecret = secret
}

func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if jwtAccessSecret == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "code": "auth_config", "message": "jwt secret is not configured"})
			c.Abort()
			return
		}

		authz := c.GetHeader("Authorization")
		parts := strings.SplitN(authz, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" || parts[1] == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "code": "unauthorized", "message": "missing bearer token"})
			c.Abort()
			return
		}

		claims := &AccessClaims{}
		token, err := jwt.ParseWithClaims(parts[1], claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(jwtAccessSecret), nil
		})
		if err != nil || !token.Valid || claims.Type != "access" || claims.UserID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "code": "unauthorized", "message": "invalid access token"})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Next()
	}
}
