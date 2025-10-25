package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

func (s *Server) meHandler(c *gin.Context) {
	if user, ok := s.getUser(c); ok {
		c.JSON(http.StatusOK, user)
	}
}

func (s *Server) updateUserPreferencesHandler(c *gin.Context) {
	user, ok := s.getUser(c)
	if !ok {
		return
	}

	var body struct {
		NotifyEmail      *string `json:"notify_email"`
		UseAccountEmail  *bool   `json:"use_account_email"`
		MarketingConsent *bool   `json:"marketing_consent"`
		DigestConsent    *bool   `json:"digest_consent"`
		DigestFrequency  *string `json:"digest_frequency"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonError(c, http.StatusBadRequest, "invalid body")
		return
	}

	// Build updates and validate
	updates := map[string]any{}
	if body.NotifyEmail != nil {
		email := strings.TrimSpace(*body.NotifyEmail)
		// allow empty if UseAccountEmail will be true
		if email == "" && (body.UseAccountEmail == nil || !*body.UseAccountEmail) {
			jsonError(c, http.StatusBadRequest, "notify_email cannot be empty unless use_account_email is true")
			return
		}
		updates["preferences.notify_email"] = email
	}
	if body.UseAccountEmail != nil {
		updates["preferences.use_account_email"] = *body.UseAccountEmail
		// If switching to account email, we can clear notify_email to avoid confusion (optional)
		if *body.UseAccountEmail {
			// Only clear if caller didn’t specify a custom notify_email in same request
			if body.NotifyEmail == nil {
				updates["preferences.notify_email"] = ""
			}
		}
	}
	if body.MarketingConsent != nil {
		updates["preferences.marketing_consent"] = *body.MarketingConsent
	}
	if body.DigestConsent != nil {
		updates["preferences.digest_consent"] = *body.DigestConsent
	}
	if body.DigestFrequency != nil {
		df := strings.ToLower(strings.TrimSpace(*body.DigestFrequency))
		switch df {
		case models.DigestFrequencyDaily, models.DigestFrequencyWeekly, models.DigestFrequencyManual:
			updates["preferences.digest_frequency"] = df
		default:
			jsonError(c, http.StatusBadRequest, "invalid digest_frequency")
			return
		}
	}

	if len(updates) == 0 {
		jsonError(c, http.StatusBadRequest, "no fields to update")
		return
	}

	updated, err := s.db.UpdateUserPreferences(c.Request.Context(), user.ID, updates)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "failed to update preferences")
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (s *Server) listUserServicesHandler(c *gin.Context) {
	user, ok := s.getUser(c)
	if !ok {
		return
	}

	// Map user's services by normalized code for quick lookup
	byCode := make(map[string]models.ServiceSubscription, len(user.Services))
	for _, serviceSubscription := range user.Services {
		byCode[strings.ToLower(serviceSubscription.Code)] = serviceSubscription
	}

	// Supported catalog
	catalog := models.ServiceCatalog

	type serviceOut struct {
		Code    string     `json:"code"`
		Name    string     `json:"name"`
		Access  string     `json:"access,omitempty"`
		Active  bool       `json:"active"`
		AddedAt *time.Time `json:"added_at,omitempty"`
		Plan    string     `json:"plan,omitempty"`
	}

	out := make([]serviceOut, 0, len(catalog))
	for _, def := range catalog {
		entry, ok := byCode[def.Code]
		var added *time.Time
		if ok && !entry.AddedAt.IsZero() {
			t := entry.AddedAt
			added = &t
		}
		plan := ""
		if ok && entry.Plan != "" {
			plan = entry.Plan
		}
		out = append(out, serviceOut{
			Code:    def.Code,
			Name:    def.Name,
			Access:  def.Access,
			Active:  ok && entry.Active,
			AddedAt: added,
			Plan:    plan,
		})
	}

	c.JSON(http.StatusOK, gin.H{"services": out})
}

func (s *Server) updateUserServicesHandler(c *gin.Context) {
	user, ok := s.getUser(c)
	if !ok {
		return
	}

	type toggle struct {
		Code   string `json:"code" binding:"required"`   // e.g. "netflix"
		Active bool   `json:"active" binding:"required"` // true to add/activate, false to remove/deactivate
	}

	var body struct {
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
		Toggle []toggle `json:"toggle"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonError(c, http.StatusBadRequest, "invalid body")
		return
	}

	// Validate service codes and normalize intent into add/remove sets
	validCodes := models.ServiceCodeSet
	toAdd := map[string]bool{}
	toRemove := map[string]bool{}
	for _, code := range body.Add {
		code = strings.ToLower(code)
		if !validCodes[code] {
			jsonError(c, http.StatusBadRequest, "invalid service code in add: "+code)
			return
		}
		toAdd[code] = true
		delete(toRemove, code)
	}
	for _, code := range body.Remove {
		code = strings.ToLower(code)
		if !validCodes[code] {
			jsonError(c, http.StatusBadRequest, "invalid service code in remove: "+code)
			return
		}
		toRemove[code] = true
		delete(toAdd, code)
	}
	for _, t := range body.Toggle {
		code := strings.ToLower(t.Code)
		if !validCodes[code] {
			jsonError(c, http.StatusBadRequest, "invalid service code in toggle: "+code)
			return
		}
		if t.Active {
			toAdd[code] = true
			delete(toRemove, code)
		} else {
			toRemove[code] = true
			delete(toAdd, code)
		}
	}
	if len(toAdd) == 0 && len(toRemove) == 0 {
		jsonError(c, http.StatusBadRequest, "no changes requested")
		return
	}

	// Merge with existing services without dropping unchanged entries
	now := time.Now()
	existing := map[string]models.ServiceSubscription{}
	for _, serviceSubscription := range user.Services {
		existing[strings.ToLower(serviceSubscription.Code)] = serviceSubscription
	}
	// Apply additions/activations
	for code := range toAdd {
		if es, ok := existing[code]; ok {
			es.Active = true
			if es.AddedAt.IsZero() {
				es.AddedAt = now
			}
			es.Code = code
			existing[code] = es
		} else {
			existing[code] = models.ServiceSubscription{Code: code, AddedAt: now, Active: true}
		}
	}
	// Apply removals/deactivations
	for code := range toRemove {
		if es, ok := existing[code]; ok {
			es.Active = false
			existing[code] = es
		}
		// If not already present, no-op; we don't create new inactive entries on remove
	}
	// Build slice back preserving all entries
	newServices := make([]models.ServiceSubscription, 0, len(existing))
	for _, v := range existing {
		newServices = append(newServices, v)
	}
	user.Services = newServices
	updatedUser, err := s.db.UpdateServices(c.Request.Context(), user.ID, user.Services)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "failed to update services")
		return
	}
	c.JSON(http.StatusOK, updatedUser)
}

func (s *Server) updateUserNotificationPreferencessHandler(c *gin.Context) {
	_, ok := s.getUser(c)
	if !ok {
		return
	}
}

func (s *Server) testNotificationHandler(c *gin.Context) {

	const (
		subject  = "Test Notification from Watchlist Notify"
		htmlBody = "<p>This is a test notification from Watchlist Notify.</p>"
	)

	// Check if notifications sender is configured
	if s.notificationsSender == nil {
		jsonError(c, http.StatusServiceUnavailable, "notification service is not configured")
		return
	}

	var body struct {
		Type          string `json:"type"`           // e.g. "digest"
		OverrideEmail string `json:"override_email"` // optional
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		jsonError(c, http.StatusBadRequest, "invalid body")
		return
	}

	// Determine recipient email based on preferences and override
	toEmail := body.OverrideEmail
	if strings.TrimSpace(toEmail) == "" {
		user, ok := s.getUser(c)
		if !ok {
			return
		}
		if user.Preferences.UseAccountEmail || strings.TrimSpace(user.Preferences.NotifyEmail) == "" {
			toEmail = user.Email
		} else {
			toEmail = user.Preferences.NotifyEmail
		}
	}

	if body.Type == "" {
		body.Type = "digest"
	}

	if err := s.notificationsSender.SendEmail(toEmail, subject, htmlBody); err != nil {
		jsonError(c, http.StatusInternalServerError, "failed to send test notification: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "test notification sent successfully",
		"sent_to": toEmail,
		"type":    body.Type,
		"sent_at": time.Now(),
	})
}
