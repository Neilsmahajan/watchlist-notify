package imdbsync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/imdb"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// The texts stored in IMDbSync.LastError and shown to the user. They are
// written here, never copied from an upstream error, so they cannot carry a
// URL, a hostname or anything else that was not meant for the user.
const (
	msgNotFound   = "IMDb list not found or not public"
	msgUnreached  = "IMDb could not be reached"
	msgPartList   = "only part of the IMDb list could be read"
	msgRepeated   = "a title is on the IMDb list twice, so titles taken off it are not removed here; remove the duplicate on IMDb"
	msgNotApplied = "the list was read but the watchlist could not be fully updated"
)

// SyncRequest is one fetched list to bring a user's watchlist in step with.
type SyncRequest struct {
	UserID primitive.ObjectID
	// Previous is the user's link before this run, nil when there is none.
	Previous *models.IMDbSync
	// SourceID is the linked user or list ID the list was fetched for.
	SourceID string
	// Mode is ModeLink or ModeSync.
	Mode Mode
	// List and FetchErr are exactly what imdb.Client.FetchList returned.
	List     imdb.List
	FetchErr error
	// At is when this run claimed its fetch of the list (the time given to
	// ClaimIMDbAttempt); zero means now. It becomes last_attempt_at, the
	// linked_at of a new link and the time on any mark the run makes. Claims
	// of one user's list are at least a minute apart, so a link made while a
	// worker run is under way is always newer than that run's marks, and
	// those marks are stale for it (see Input.LinkedAt).
	At time.Time
	// LookupBudget and DryRun are passed on to the reconcile.
	LookupBudget time.Duration
	DryRun       bool
}

// SyncResult is the outcome of SyncList.
type SyncResult struct {
	// Result is the reconcile's result; zero when the list could not be read.
	Result Result
	// State is the user's link as it should be stored after this run.
	State models.IMDbSync
	// Err is the reconcile's error, if it stopped early.
	Err error
}

// SyncList reconciles a fetched list and works out the link state to store.
//
// What the fetch returned decides how far the run goes:
//
//   - Nothing could be read (the list is missing or private, or IMDb failed
//     before the first page): nothing is reconciled at all. Status
//     "not_found" or "failed".
//   - Part of the list was read: its titles are added, since a title on any
//     page is certainly on the list, but nothing is marked or removed.
//     Status "partial".
//   - The whole list was read: the full reconcile runs. Status "ok", or
//     "partial" when some titles could not be looked up or written yet.
func (r *Reconciler) SyncList(ctx context.Context, req SyncRequest) SyncResult {
	// Milliseconds, like the database stores, so the state handed back is
	// exactly the state that will be read later.
	at := req.At
	if at.IsZero() {
		at = r.now()
	}
	at = at.UTC().Truncate(time.Millisecond)
	state := models.IMDbSync{SourceID: req.SourceID, LinkedAt: at}
	if req.Previous != nil && req.Previous.SourceID == req.SourceID {
		// Same list as before: keep when it was linked, when it last synced
		// and the counts of the last run that read it.
		state = *req.Previous
	}
	state.LastAttemptAt = &at

	if req.FetchErr != nil && len(req.List.Entries) == 0 {
		state.LastStatus, state.LastError = models.IMDbStatusFailed, msgUnreached
		if errors.Is(req.FetchErr, imdb.ErrNotFound) {
			state.LastStatus, state.LastError = models.IMDbStatusNotFound, msgNotFound
		}
		return SyncResult{State: state}
	}

	ids := make([]string, len(req.List.Entries))
	for i, entry := range req.List.Entries {
		ids[i] = entry.IMDbID
	}
	complete := req.FetchErr == nil && req.List.Complete
	res, err := r.Run(ctx, Request{
		UserID:       req.UserID,
		Mode:         req.Mode,
		IDs:          ids,
		Complete:     complete,
		SourceID:     state.SourceID,
		LinkedAt:     state.LinkedAt,
		At:           at,
		LookupBudget: req.LookupBudget,
		DryRun:       req.DryRun,
	})

	// Total is the number of titles on the list as IMDb reports it, which is
	// more than was read when the fetch was partial.
	state.Total = max(req.List.Total-req.List.Other-req.List.Repeated, res.Total)
	state.Added = res.Added
	state.Removed = res.Removed
	state.Unmatched = res.Unmatched
	switch {
	case !complete && req.FetchErr == nil && req.List.Repeated > 0:
		// The client cannot tell a title listed twice from two pages that
		// overlap, so it never calls such a list complete (see imdb.List).
		state.LastStatus, state.LastError = models.IMDbStatusPartial, msgRepeated
	case !complete:
		state.LastStatus, state.LastError = models.IMDbStatusPartial, msgPartList
	case err != nil || res.LinkChanged:
		// LinkChanged: the user unlinked or linked afresh while this ran, so
		// the removals were left undone. The caller stores a status only on
		// the link the run was for, so this one is normally never seen.
		state.LastStatus, state.LastError = models.IMDbStatusPartial, msgNotApplied
	case res.Pending > 0:
		state.LastStatus = models.IMDbStatusPartial
		state.LastError = fmt.Sprintf("%d titles could not be looked up yet; the next sync tries again", res.Pending)
	default:
		state.LastStatus, state.LastError = models.IMDbStatusOK, ""
	}
	if complete && err == nil && !res.LinkChanged {
		state.LastSyncedAt = &at
	}
	return SyncResult{Result: res, State: state, Err: err}
}
