# Worker

One binary for the scheduled background work, deployed as a Cloud Run job.
Each run executes its tasks in order and exits:

| Task      | What it does                                                                                              |
| --------- | --------------------------------------------------------------------------------------------------------- |
| `refresh` | Fetches every title on any watchlist from TMDb, records what changed, and replaces the stored snapshot.    |
| `digest`  | Emails users who are due about titles that newly arrived on their services. Dry run unless told otherwise. |

`refresh` runs before `digest` so a digest is built from today's data. A slot
between them is reserved for a future `imdb` task (ratings); selecting it today
is an error. A task that fails does not stop the ones after it; the process
exits non-zero if any task failed.

```bash
make worker           # refresh, then digest (dry run)
make worker-refresh   # refresh only
make worker-digest    # digest only
go run ./cmd/worker -tasks refresh,digest
```

## Environment

| Variable                | Used by | Default                       | Meaning                                                                                              |
| ----------------------- | ------- | ----------------------------- | ---------------------------------------------------------------------------------------------------- |
| `WORKER_TASKS`          | both    | `refresh,digest`              | Comma-separated tasks. Always run in the order above. The `-tasks` flag overrides it.                 |
| `MONGODB_URI`, `DB_*`   | both    |                               | Same database settings as the API.                                                                   |
| `DEFAULT_REGION`        | both    | `US`                          | Region for users without one; always refreshed.                                                      |
| `TMDB_API_KEY`          | refresh | required                      | TMDb key.                                                                                            |
| `REFRESH_MIN_AGE`       | refresh | `20h`                         | Titles checked more recently than this are left alone. `0` refetches everything on every run.         |
| `DIGEST_MODE`           | digest  | `dry-run`                     | `dry-run` logs what would be sent and writes nothing. `send` delivers through Postmark.              |
| `DIGEST_ALLOWLIST`      | digest  | empty                         | Comma-separated addresses that may be emailed in `send` mode. Empty means nobody.                    |
| `POSTMARK_SERVER_TOKEN` | digest  | required in `send` mode       | Postmark server token. Not read in a dry run.                                                        |
| `FRONTEND_URL`          | digest  | `https://watchlistnotify.com` | Site the email links to (`/watchlist`, `/settings`).                                                 |

## Refresh

For every distinct title on a watchlist and every region a user has (plus the
default region), the task makes one TMDb request
(`/{movie|tv}/{id}?append_to_response=external_ids,watch/providers`) and
compares the result with the stored snapshot in the `titles` collection:

- The first snapshot for a title and region is a **baseline**. It is stored
  and produces no events, so backfilling an existing database announces
  nothing.
- After that, each service that started or stopped streaming the title
  becomes a row in `availability_events`. Event IDs are derived from the title,
  region, service, direction and the version of the snapshot being replaced,
  so a retry or an overlapping run cannot record the same change twice.
  Events are kept for 400 days, longer than the longest digest interval a
  user can choose (12 months).
- Events are written before the snapshot is replaced, and the replace is
  conditional on the snapshot still being the version that was read. The two
  writes are not atomic. If the second fails, the event exists but the
  snapshot does not show the change yet, so the digest leaves the title out.
  The next run diffs against the same snapshot, finds the same change under
  the same ID, keeps the event that is already there (with its original
  `detected_at`) and replaces the snapshot. The digest then picks the event
  up, even if it has sent other mail in between (see "Digest").
- A failed fetch, or a response without provider data, changes nothing. It
  is never read as "removed everywhere". A title TMDb no longer has is marked
  `not_found_at` and keeps its last snapshot.

The run ends with a `refresh summary` log line: `titles`, `fresh` (skipped as
recently checked), `refreshed`, `baselines`, `changed`, `events`, `not_found`,
`superseded`, `failures`, `skipped` (not reached before the run was cancelled).
Two more lines keep the provider mapping honest: `providers matched by name
only` (add their TMDb IDs to `internal/models/service_mapping.go`) and
`streaming providers without a service code`.

## Digest

A user is due when digests are enabled, consent is given and
`next_scheduled_at` has passed. A due user gets an email only if at least one
title on their list **newly** became streamable on a service they have:

- there is an "added" event for one of their active services, in their region,
  that they have not been sent before, detected no earlier than 7 days before
  their last digest (or within one interval, for a first one);
- it was detected after they put the title on their list;
- the title is still on that service now;
- the service did not drop the same title within the previous 7 days (provider
  data flickers; a title that vanishes for a day and returns is not news);
- the item is not marked finished.

With nothing new, nothing is sent and the schedule is left alone, so the user
is looked at again on the next run. One email lists at most 20 titles.

**Why the window overlaps, and the delivery log.** A digest cannot start
exactly where the last one stopped. An event is stamped with the time the
change was detected but becomes readable only when its write commits, and
becomes true only when the snapshot write behind it succeeds, which may be a
run later. Either way an event can show up carrying a time the last digest has
already passed. So each digest re-reads the 7 days before the last one, and the
`digest_deliveries` collection records which events each user was sent (one
row per user and event, unique, kept 400 days). An event with a row is never
announced to that user again.

### Safety switches

- **Dry run is the default.** Without `DIGEST_MODE=send` the task builds no
  mail client, sends nothing and writes nothing. It logs how many users it
  would email and how many titles.
- **Sending needs an allowlist.** In `send` mode a user is emailed only if
  their account email is in `DIGEST_ALLOWLIST` **and** the address the digest
  is delivered to is the account email or is itself in the list. The second
  half matters because the notification address is a preference the user
  types in and nobody verifies; an allowlisted account cannot aim its digest
  at an address that is not listed. Due users who fail either check are
  counted (`not_allowlisted`) and skipped, schedule untouched.
- **At most one email per user per interval.** Before sending, the run moves
  the user's schedule forward with a compare-and-set on the values it read.
  Of two overlapping runs, one wins that write and only the winner sends.
- **Record, then send.** The winner writes the delivery rows for the events it
  is about to announce, and only then sends. If the rows cannot be written,
  nothing is sent and the claim is released.
- If Postmark definitely rejected the message (a 4xx, or the connection was
  never made) the delivery rows are deleted, then the schedule is put back, and
  the next run retries. If the outcome is unknown (timeout, 5xx) the claim and
  the rows are kept: the user may miss one digest rather than risk receiving
  the same one twice.

Logs carry user IDs and counts, never addresses or email content. The run
ends with a `digest summary` line: `mode`, `due`, `recipients`, `titles`,
`sent`, `dry_run`, `not_allowlisted`, `nothing_new`, `claim_lost`,
`send_failed`, `send_uncertain`, `errors`.

## Deploy

```bash
# Build, push and update the Cloud Run job (env vars from cloudrun.env)
./scripts/release-worker.sh --env-file cloudrun.env

# Or build with Cloud Build
./scripts/release-worker.sh --cloud-build --env-file cloudrun.env
```

The image and the job keep their original name, `digest-worker`, so the
existing job and its Cloud Scheduler trigger carry over. Override with
`WORKER_IMAGE` / `WORKER_SERVICE` to use different names.

### Running it by hand

```bash
# Backfill or refresh availability only
gcloud run jobs execute digest-worker --region "${REGION}" \
  --update-env-vars WORKER_TASKS=refresh --wait

# Digest dry run: logs who would be emailed, sends nothing
gcloud run jobs execute digest-worker --region "${REGION}" \
  --update-env-vars WORKER_TASKS=digest,DIGEST_MODE=dry-run --wait

# Read the summaries
gcloud logging read \
  'resource.type=cloud_run_job AND resource.labels.job_name=digest-worker AND jsonPayload.msg:"summary"' \
  --limit 10 --format json
```

`--update-env-vars` on `execute` applies to that execution only.

### Schedule

Production runs the job once a day (09:00 UTC). It is also safe to trigger
more often: `refresh` skips titles it checked within `REFRESH_MIN_AGE`, and
`digest` only acts on users who are due.

```bash
gcloud scheduler jobs create http digest-worker-daily \
  --location "${REGION}" \
  --schedule "0 9 * * *" \
  --uri "https://${REGION}-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/${PROJECT_ID}/jobs/digest-worker:run" \
  --http-method POST \
  --oauth-service-account-email "digest-scheduler@${PROJECT_ID}.iam.gserviceaccount.com"
```

The service account needs `roles/run.invoker` on the job.
