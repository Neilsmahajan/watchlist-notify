package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

var (
	oauthConfig *oauth2.Config
	jwtSecret   []byte
)

func Init() {
	oauthConfig = &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("BASE_URL") + "/auth/google/callback",
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
			"openid",
		},
		Endpoint: google.Endpoint,
	}
	jwtSecret = []byte(os.Getenv("JWT_SECRET"))
}

// Generate a random state string for OAuth2 flow
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func LoginHandler(c *gin.Context) {
	state, err := generateState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to gen state"})
		return
	}
	// Store state in a secure cookie (short lived)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "oauthstate",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // set true in production (HTTPS)
		MaxAge:   300,
		SameSite: http.SameSiteLaxMode,
	})
	url := oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

func CallbackHandler(c *gin.Context, db database.Service) {
	stateCookie, err := c.Request.Cookie("oauthstate")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing state cookie"})
		return
	}
	if c.Query("state") != stateCookie.Value {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid oauth state"})
		return
	}
	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing code"})
		return
	}
	token, err := oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code exchange failed"})
		return
	}

	// Fetch userinfo (email)
	client := oauthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil || resp.StatusCode != 200 {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed userinfo"})
		return
	}
	defer resp.Body.Close()

	type googleUserInfo struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Email   string `json:"email"`
		Picture string `json:"picture"`
	}
	var googleUser googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&googleUser); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode userinfo"})
		return
	}

	user := models.User{
		GoogleID: googleUser.ID,
		Email:    googleUser.Email,
		Name:     googleUser.Name,
		Picture:  googleUser.Picture,
	}

	// Upsert user (persist before issuing token)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.UpsertUser(ctx, &user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "persist user"})
		return
	}

	// Issue JWT cookie
	jwtToken, err := issueJWT(user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "jwt"})
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "auth_token",
		Value:    jwtToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // true in prod
		MaxAge:   3600,
		SameSite: http.SameSiteLaxMode,
	})
	c.JSON(http.StatusOK, gin.H{"message": "login success", "email": user.Email, "token": jwtToken})
}

func issueJWT(email string) (string, error) {
	if len(jwtSecret) == 0 {
		return "", errors.New("jwt secret not set")
	}
	claims := Claims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

func ParseJWT(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		return jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, errors.New("invalid token")
}
