package server

import (
	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/auth"
)

// authLoginHandler starts the Google OAuth login flow.
func (s *Server) authLoginHandler(c *gin.Context) {
	auth.LoginHandler(c)
}

// authCallbackHandler completes the OAuth flow and upserts the user.
func (s *Server) authCallbackHandler(c *gin.Context) {
	auth.CallbackHandler(c, s.db)
}

// logoutHandler clears the auth cookie.
func (s *Server) logoutHandler(c *gin.Context) {
	auth.LogoutHandler(c)
}
