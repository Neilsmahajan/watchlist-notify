# Digest Worker Service

A scheduled Cloud Run service that sends personalized watchlist digest emails to users.

## Quick Start

### Deploy Worker

```bash
# Deploy worker to Cloud Run
./scripts/release-worker.sh

# Or with Cloud Build
./scripts/release-worker.sh --cloud-build
```

### Set Environment Variables

```bash
gcloud run services update digest-worker \
  --region us-east1 \
  --set-env-vars "MONGODB_URI=mongodb+srv://...,POSTMARK_SERVER_TOKEN=..."
```

### Create Scheduler Job (First Time Only)

```bash
# Get worker URL
WORKER_URL=$(gcloud run services describe digest-worker --region us-east1 --format 'value(status.url)')

# Create service account
gcloud iam service-accounts create digest-scheduler

# Grant invoke permission
gcloud run services add-iam-policy-binding digest-worker \
  --region us-east1 \
  --member "serviceAccount:digest-scheduler@watchlist-notify-470822.iam.gserviceaccount.com" \
  --role "roles/run.invoker"

# Create scheduler job (runs every hour)
gcloud scheduler jobs create http digest-worker-hourly \
  --location us-east1 \
  --schedule "0 * * * *" \
  --uri "${WORKER_URL}" \
  --http-method POST \
  --oidc-service-account-email "digest-scheduler@watchlist-notify-470822.iam.gserviceaccount.com" \
  --oidc-token-audience "${WORKER_URL}"
```

### Manual Test

```bash
# Trigger scheduler manually
gcloud scheduler jobs run digest-worker-hourly --location us-east1

# View logs
gcloud run logs read digest-worker --region us-east1 --limit 50
```

## How It Works

1. Cloud Scheduler triggers worker every hour
2. Worker queries users with `next_scheduled_at <= now` and `digest_consent = true`
3. For each user:
   - Fetch watchlist items
   - Generate HTML email with available content
   - Send via Postmark
   - Update `last_sent_at` and `next_scheduled_at`

## Monitoring

```bash
# Check scheduler status
gcloud scheduler jobs describe digest-worker-hourly --location us-east1

# View recent logs
gcloud run logs read digest-worker --region us-east1 --limit 50

# Check for errors
gcloud run logs read digest-worker --region us-east1 | grep ERROR
```

## Development

```bash
# Build locally
docker build -f Dockerfile.worker -t digest-worker .

# Run locally (requires env vars)
docker run --rm \
  -e MONGODB_URI="..." \
  -e POSTMARK_SERVER_TOKEN="..." \
  digest-worker
```

## Architecture

See [`docs/EMAIL_SERVICE_ARCHITECTURE.md`](../docs/EMAIL_SERVICE_ARCHITECTURE.md) for detailed architecture documentation.

## Deployment Guide

See [`docs/DIGEST_WORKER_DEPLOYMENT.md`](../docs/DIGEST_WORKER_DEPLOYMENT.md) for comprehensive deployment instructions.
