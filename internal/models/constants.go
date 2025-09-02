package models

// Digest frequencies
const (
	DigestFrequencyDaily  = "daily"
	DigestFrequencyWeekly = "weekly"
	DigestFrequencyManual = "manual"
)

// Watchlist item types
const (
	WatchlistTypeMovie = "movie"
	WatchlistTypeShow  = "show"
)

// Watchlist item statuses
const (
	WatchlistStatusPlanned  = "planned"
	WatchlistStatusWatching = "watching"
	WatchlistStatusFinished = "finished"
)

// Notification types (future use but defined early for consistency)
const (
	NotificationTypeDigest  = "digest"
	NotificationTypeInstant = "instant"
)

// Supported service codes (phase 1 subset). Use lowercase stable codes.
const (
	ServiceNetflix    = "netflix"
	ServicePrimeVideo = "prime_video"
	ServiceHulu       = "hulu"
	ServiceDisneyPlus = "disney_plus"
	ServiceMax        = "max"
)
