package main

import (
	"context"
	"log"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/digest"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/notifications"
	"github.com/neilsmahajan/watchlist-notify/internal/server"
)

func main() {
	log.Println("Starting digest worker...")

	// Initialize dependencies
	db := database.New()
	sender, err := notifications.New(notifications.DefaultFromEmail, notifications.DefaultMessageStream)
	if err != nil {
		log.Fatalf("Failed to initialize email sender: %v", err)
	}
	generator := digest.NewGenerator(db)

	// Run digest processing
	ctx := context.Background()
	if err := processDigests(ctx, db, sender, generator); err != nil {
		log.Fatalf("Failed to process digests: %v", err)
	}

	log.Println("Digest worker completed successfully")
}

func processDigests(ctx context.Context, db database.Service, sender notifications.EmailSender, generator *digest.Generator) error {
	now := time.Now()
	users, err := db.GetUsersForDigest(ctx, now)
	if err != nil {
		return err
	}

	log.Printf("Found %d users eligible for digest emails", len(users))

	successCount := 0
	errorCount := 0

	for _, user := range users {
		if err := processUserDigest(ctx, db, sender, generator, user); err != nil {
			log.Printf("Failed to process digest for user %s (%s): %v", user.ID.Hex(), user.Email, err)
			errorCount++
			continue
		}
		successCount++
	}

	log.Printf("Digest processing complete: %d successful, %d errors", successCount, errorCount)
	return nil
}

func processUserDigest(ctx context.Context, db database.Service, sender notifications.EmailSender, generator *digest.Generator, user *models.User) error {
	// Generate digest content
	digestData, err := generator.GenerateForUser(ctx, user)
	if err != nil {
		return err
	}

	// Skip if no content to share
	if digestData.IsEmpty() {
		log.Printf("Skipping digest for user %s: no new content", user.Email)
		// Still update timestamps to avoid checking again soon
		now := time.Now()
		nextTime := server.CalculateNextDigestTime(
			user.Preferences.Digest.Interval,
			user.Preferences.Digest.IntervalUnit,
			&now,
		)
		return db.UpdateDigestTimestamps(ctx, user.ID, now, nextTime)
	}

	// Determine recipient email
	email := user.Email
	if !user.Preferences.UseAccountEmail && user.Preferences.NotifyEmail != "" {
		email = user.Preferences.NotifyEmail
	}

	// Generate HTML email
	subject := digestData.Subject()
	htmlBody := digestData.HTML()

	// Send email via Postmark
	if err := sender.SendEmail(email, subject, htmlBody); err != nil {
		return err
	}

	// Update timestamps
	now := time.Now()
	nextTime := server.CalculateNextDigestTime(
		user.Preferences.Digest.Interval,
		user.Preferences.Digest.IntervalUnit,
		&now,
	)

	log.Printf("Sent digest to %s, next scheduled: %s", email, nextTime.Format(time.RFC3339))
	return db.UpdateDigestTimestamps(ctx, user.ID, now, nextTime)
}
