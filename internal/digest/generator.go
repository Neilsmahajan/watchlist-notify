package digest

import (
	"context"
	"fmt"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

// Generator creates digest email content for users
type Generator struct {
	db database.Service
}

// NewGenerator creates a new digest generator
func NewGenerator(db database.Service) *Generator {
	return &Generator{db: db}
}

// DigestData holds all information for a user's digest email
type DigestData struct {
	User           *models.User
	AvailableItems []*models.WatchlistItem
	WatchingCount  int
	AvailableCount int
	TotalCount     int
	GeneratedAt    time.Time
}

// IsEmpty returns true if there's no meaningful content to send
func (d *DigestData) IsEmpty() bool {
	return len(d.AvailableItems) == 0
}

// Subject returns the email subject line
func (d *DigestData) Subject() string {
	if len(d.AvailableItems) == 0 {
		return "Your Watchlist Digest"
	}
	if len(d.AvailableItems) == 1 {
		return "1 item from your watchlist is now available!"
	}
	return fmt.Sprintf("%d items from your watchlist are now available!", len(d.AvailableItems))
}

// HTML generates the HTML email body
func (d *DigestData) HTML() string {
	// TODO: Use proper HTML templates
	// For now, a simple implementation
	html := `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Watchlist Digest</title>
</head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
    <div style="background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); padding: 30px; text-align: center; border-radius: 8px 8px 0 0;">
        <h1 style="color: white; margin: 0; font-size: 28px;">🎬 Watchlist Digest</h1>
        <p style="color: rgba(255,255,255,0.9); margin: 10px 0 0 0;">Your personalized streaming updates</p>
    </div>
    
    <div style="background: #ffffff; padding: 30px; border: 1px solid #e5e7eb; border-top: none; border-radius: 0 0 8px 8px;">
`

	if len(d.AvailableItems) > 0 {
		html += fmt.Sprintf(`
        <h2 style="color: #10b981; margin-top: 0;">✨ %d New Availability Alert%s</h2>
        <p style="color: #6b7280;">Great news! These items from your watchlist are now available to stream:</p>
        
        <div style="margin: 20px 0;">
`, len(d.AvailableItems), pluralize(len(d.AvailableItems)))

		for _, item := range d.AvailableItems {
			itemType := "Movie"
			if item.Type == models.WatchlistTypeShow {
				itemType = "TV Show"
			}

			html += fmt.Sprintf(`
            <div style="background: #f9fafb; border-left: 4px solid #667eea; padding: 15px; margin-bottom: 15px; border-radius: 4px;">
                <h3 style="margin: 0 0 5px 0; color: #1f2937; font-size: 18px;">%s</h3>
                <p style="margin: 5px 0; color: #6b7280; font-size: 14px;">%s</p>
                <p style="margin: 10px 0 0 0; color: #4b5563; font-size: 14px;"><strong>Available on:</strong> %s</p>
            </div>
`, item.Title, itemType, formatServices(item))
		}

		html += `
        </div>
`
	}

	html += fmt.Sprintf(`
        <div style="background: #f3f4f6; padding: 20px; border-radius: 8px; margin: 30px 0;">
            <h3 style="margin: 0 0 15px 0; color: #374151;">📊 Your Watchlist Stats</h3>
            <div style="display: grid; grid-template-columns: repeat(3, 1fr); gap: 15px;">
                <div style="text-align: center;">
                    <div style="font-size: 24px; font-weight: bold; color: #667eea;">%d</div>
                    <div style="font-size: 12px; color: #6b7280; margin-top: 5px;">Total Items</div>
                </div>
                <div style="text-align: center;">
                    <div style="font-size: 24px; font-weight: bold; color: #10b981;">%d</div>
                    <div style="font-size: 12px; color: #6b7280; margin-top: 5px;">Available</div>
                </div>
                <div style="text-align: center;">
                    <div style="font-size: 24px; font-weight: bold; color: #f59e0b;">%d</div>
                    <div style="font-size: 12px; color: #6b7280; margin-top: 5px;">Watching</div>
                </div>
            </div>
        </div>
        
        <div style="text-align: center; margin-top: 30px;">
            <a href="https://watchlistnotify.com/watchlist" style="background: #667eea; color: white; padding: 12px 30px; text-decoration: none; border-radius: 6px; display: inline-block; font-weight: 600;">View Full Watchlist</a>
        </div>
        
        <div style="margin-top: 30px; padding-top: 20px; border-top: 1px solid #e5e7eb; text-align: center; color: #9ca3af; font-size: 12px;">
            <p>You're receiving this because you enabled digest emails in your <a href="https://watchlistnotify.com/settings" style="color: #667eea;">notification preferences</a>.</p>
            <p style="margin-top: 10px;">© 2025 Watchlist Notify. All rights reserved.</p>
        </div>
    </div>
</body>
</html>
`, d.TotalCount, d.AvailableCount, d.WatchingCount)

	return html
}

func pluralize(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func formatServices(item *models.WatchlistItem) string {
	// TODO: Implement proper service name formatting
	// For now, return a placeholder
	return "Your streaming services"
}

// GenerateForUser creates digest data for a specific user
func (g *Generator) GenerateForUser(ctx context.Context, user *models.User) (*DigestData, error) {
	// Get user's watchlist
	watchlist, err := g.db.ListWatchlistItems(ctx, models.ListWatchlistOptions{
		UserID: user.ID,
	})
	if err != nil {
		return nil, err
	}

	data := &DigestData{
		User:           user,
		TotalCount:     len(watchlist),
		GeneratedAt:    time.Now(),
		AvailableItems: make([]*models.WatchlistItem, 0),
	}

	// Count items and find newly available ones
	for _, item := range watchlist {
		if item.Status == models.WatchlistStatusWatching {
			data.WatchingCount++
		}

		// TODO: Implement proper availability checking
		// For now, items are considered "newly available" if they were added/updated recently
		// and have availability data
		// This is a placeholder - you'll want to track "notified" state
		if item.Availability != nil && len(item.Availability) > 0 {
			data.AvailableCount++
			// Only include in digest if recently became available
			// For MVP, include all available items
			data.AvailableItems = append(data.AvailableItems, item)
		}
	}

	return data, nil
}
