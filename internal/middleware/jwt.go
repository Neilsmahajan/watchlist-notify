package middleware

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"github.com/auth0/go-jwt-middleware/v2/jwks"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/gin-gonic/gin"
)

type ValidatedClaimsKey string

const auth0ValidatedClaimsContextKey ValidatedClaimsKey = "auth0_validated_claims"

// CustomClaims contains custom data we want from the token.
type CustomClaims struct {
	Scope string `json:"scope"`
	// Standard OIDC-style fields (present in ID tokens or if explicitly added)
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Picture string `json:"picture,omitempty"`
	// Namespaced claims commonly used in Auth0 access tokens for custom APIs
	EmailNs   string `json:"https://watchlistnotify.com/email,omitempty"`
	NameNs    string `json:"https://watchlistnotify.com/name,omitempty"`
	PictureNs string `json:"https://watchlistnotify.com/picture,omitempty"`
}

// Validate does nothing for this example, but we need
// it to satisfy validator.CustomClaims interface.
func (c CustomClaims) Validate(ctx context.Context) error {
	return nil
}

// HasScope checks whether our claims have a specific scope.
func (c CustomClaims) HasScope(expectedScope string) bool {
	result := strings.Split(c.Scope, " ")
	for i := range result {
		if result[i] == expectedScope {
			return true
		}
	}

	return false
}

// EnsureValidToken returns a Gin middleware that validates incoming JWTs
// against Auth0 using RS256 and the configured issuer/audience.
func EnsureValidToken() gin.HandlerFunc {
	issuerURL, err := url.Parse("https://" + os.Getenv("AUTH0_DOMAIN") + "/")
	if err != nil {
		log.Fatalf("Failed to parse the issuer url: %v", err)
	}

	provider := jwks.NewCachingProvider(issuerURL, 5*time.Minute)

	jwtValidator, err := validator.New(
		provider.KeyFunc,
		validator.RS256,
		issuerURL.String(),
		[]string{os.Getenv("AUTH0_AUDIENCE")},
		validator.WithCustomClaims(
			func() validator.CustomClaims {
				return &CustomClaims{}
			},
		),
		validator.WithAllowedClockSkew(time.Minute),
	)
	if err != nil {
		log.Fatalf("Failed to set up the jwt validator")
	}

	return func(c *gin.Context) {
		// Extract Bearer token from Authorization header
		authz := c.GetHeader("Authorization")
		var token string
		if authz != "" {
			parts := strings.SplitN(authz, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				token = strings.TrimSpace(parts[1])
			}
		}
		// Fallback to cookie for backwards compatibility if needed
		if token == "" {
			if cookie, err := c.Request.Cookie("auth_token"); err == nil {
				token = cookie.Value
			}
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Missing bearer token."})
			return
		}

		validated, err := jwtValidator.ValidateToken(c.Request.Context(), token)
		if err != nil {
			log.Printf("Encountered error while validating JWT: %v", err)
			c.Header("Content-Type", "application/json")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Failed to validate JWT."})
			return
		}

		// Attach validated claims to both request context and gin context for downstream use
		ctx := context.WithValue(c.Request.Context(), auth0ValidatedClaimsContextKey, validated)
		c.Request = c.Request.WithContext(ctx)
		if vc, ok := validated.(*validator.ValidatedClaims); ok {
			// Commonly useful fields
			c.Set("sub", vc.RegisteredClaims.Subject)
			if cc, ok := vc.CustomClaims.(*CustomClaims); ok {
				c.Set("scope", cc.Scope)
				// Prefer standard fields, fallback to namespaced
				email := cc.Email
				if email == "" {
					email = cc.EmailNs
				}
				name := cc.Name
				if name == "" {
					name = cc.NameNs
				}
				picture := cc.Picture
				if picture == "" {
					picture = cc.PictureNs
				}
				if email != "" {
					c.Set("user_email", email)
				}
				if name != "" {
					c.Set("name", name)
				}
				if picture != "" {
					c.Set("picture", picture)
				}
			}
		}

		c.Next()
	}
}
