package models

// Digest interval units as stored in DigestSettings.IntervalUnit.
const (
	DigestUnitDays   = "days"
	DigestUnitWeeks  = "weeks"
	DigestUnitMonths = "months"
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
