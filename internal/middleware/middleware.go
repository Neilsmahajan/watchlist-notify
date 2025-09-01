package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/auth"
)

// AuthRequired is a simple middleware that checks for auth_token cookie or Bearer token
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ""
		if cookie, err := c.Request.Cookie("auth_token"); err == nil {
			token = cookie.Value
		}
		if token == "" {
			// Check Authorization header
			authz := c.GetHeader("Authorization")
			if parts := strings.SplitN(authz, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				token = parts[1]
			}
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}
		claims, err := auth.ParseJWT(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		// put claims into context for handlers
		c.Set("user_email", claims.Email)
		c.Next()
	}
}
