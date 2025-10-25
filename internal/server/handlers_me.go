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
		NotifyEmail        *string `json:"notify_email"`
		UseAccountEmail    *bool   `json:"use_account_email"`
		MarketingConsent   *bool   `json:"marketing_consent"`
		DigestConsent      *bool   `json:"digest_consent"`
		DigestEnabled      *bool   `json:"digest_enabled"`
		DigestInterval     *int    `json:"digest_interval"`      // 1-31 for days, 1-12 for weeks/months
		DigestIntervalUnit *string `json:"digest_interval_unit"` // "days", "weeks", "months"
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonError(c, http.StatusBadRequest, "invalid body")
		return
	}

	updates := map[string]any{}
	if body.NotifyEmail != nil {
		email := strings.TrimSpace(*body.NotifyEmail)
		if email == "" && (body.UseAccountEmail == nil || !*body.UseAccountEmail) {
			jsonError(c, http.StatusBadRequest, "notify_email cannot be empty unless use_account_email is true")
			return
		}
		updates["preferences.notify_email"] = email
	}
	if body.UseAccountEmail != nil {
		updates["preferences.use_account_email"] = *body.UseAccountEmail
		if *body.UseAccountEmail {
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
	if body.DigestEnabled != nil {
		updates["preferences.digest.enabled"] = *body.DigestEnabled
	}
	if body.DigestInterval != nil {
		interval := *body.DigestInterval
		if interval < 1 {
			jsonError(c, http.StatusBadRequest, "digest_interval must be at least 1")
			return
		}
		// Validate based on unit if also provided
		if body.DigestIntervalUnit != nil {
			unit := strings.ToLower(*body.DigestIntervalUnit)
			switch unit {
			case "days":
				if interval > 31 {
					jsonError(c, http.StatusBadRequest, "digest_interval for days cannot exceed 31")
					return
				}
			case "weeks", "months":
				if interval > 12 {
					jsonError(c, http.StatusBadRequest, "digest_interval for weeks/months cannot exceed 12")
					return
				}
			}
		}
		updates["preferences.digest.interval"] = interval
	}
	if body.DigestIntervalUnit != nil {
		unit := strings.ToLower(strings.TrimSpace(*body.DigestIntervalUnit))
		if unit != "days" && unit != "weeks" && unit != "months" {
			jsonError(c, http.StatusBadRequest, "digest_interval_unit must be 'days', 'weeks', or 'months'")
			return
		}
		updates["preferences.digest.interval_unit"] = unit
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

	user, ok := s.getUser(c)
	if !ok {
		return
	}

	var body struct {
		Type          string `json:"type"`           // e.g. "digest"
		OverrideEmail string `json:"override_email"` // optional - must be user's account or notify email
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		// Allow empty body for simple test
		body.Type = "digest"
	}

	// Determine recipient email based on preferences and override
	toEmail := strings.TrimSpace(body.OverrideEmail)

	// Validate override email if provided - must match user's configured emails
	if toEmail != "" {
		validEmails := map[string]bool{
			strings.ToLower(user.Email): true,
		}
		if user.Preferences.NotifyEmail != "" {
			validEmails[strings.ToLower(user.Preferences.NotifyEmail)] = true
		}
		if !validEmails[strings.ToLower(toEmail)] {
			jsonError(c, http.StatusBadRequest, "override_email must be your account email or configured notification email")
			return
		}
	} else {
		// Use preference-based email selection
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
