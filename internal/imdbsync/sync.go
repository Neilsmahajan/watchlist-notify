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
	at := r.now().UTC()
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
		LookupBudget: req.LookupBudget,
		DryRun:       req.DryRun,
	})

	// Total is the number of titles on the list as IMDb reports it, which is
	// more than was read when the fetch was partial.
	state.Total = max(req.List.Total-req.List.Other, res.Total)
	state.Added = res.Added
	state.Removed = res.Removed
	state.Unmatched = res.Unmatched
	switch {
	case !complete:
		state.LastStatus, state.LastError = models.IMDbStatusPartial, msgPartList
	case err != nil:
		state.LastStatus, state.LastError = models.IMDbStatusPartial, msgNotApplied
	case res.Pending > 0:
		state.LastStatus = models.IMDbStatusPartial
		state.LastError = fmt.Sprintf("%d titles could not be looked up yet; the next sync tries again", res.Pending)
	default:
		state.LastStatus, state.LastError = models.IMDbStatusOK, ""
	}
	if complete && err == nil {
		state.LastSyncedAt = &at
	}
	return SyncResult{Result: res, State: state, Err: err}
}
