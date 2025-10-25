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
	DigestIntervalDay   = "day"
	DigestIntervalWeek  = "week"
	DigestIntervalMonth = "month"
)
