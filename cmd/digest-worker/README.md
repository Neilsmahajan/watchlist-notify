# Digest Worker Service

A scheduled Cloud Run Job that sends personalized watchlist digest emails to users.

## Quick Start

### Deploy Worker

```bash
# Deploy worker as Cloud Run Job (with env vars from cloudrun.env)
./scripts/release-worker.sh --env-file cloudrun.env

# Or with Cloud Build
./scripts/release-worker.sh --cloud-build --env-file cloudrun.env
```

### Create Scheduler Job (First Time Only)

```bash
# Get project details
PROJECT_ID="watchlist-notify-470822"
REGION="us-east1"

# Create service account
gcloud iam service-accounts create digest-scheduler \
  --display-name "Digest Worker Scheduler"

# Grant invoke permission
gcloud run jobs add-iam-policy-binding digest-worker \
  --region ${REGION} \
  --member "serviceAccount:digest-scheduler@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role "roles/run.invoker"

# Create scheduler job (runs every hour)
gcloud scheduler jobs create http digest-worker-hourly \
  --location ${REGION} \
  --schedule "0 * * * *" \
  --uri "https://${REGION}-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/${PROJECT_ID}/jobs/digest-worker:run" \
  --http-method POST \
  --oauth-service-account-email "digest-scheduler@${PROJECT_ID}.iam.gserviceaccount.com" \
  --description "Runs digest email worker every hour"
```

### Manual Test

```bash
# Execute job manually
gcloud run jobs execute digest-worker --region us-east1

# View logs
gcloud logging read "resource.type=cloud_run_job AND resource.labels.job_name=digest-worker" \
  --limit 50 \
  --format "table(timestamp,severity,textPayload)"

# Trigger via scheduler
gcloud scheduler jobs run digest-worker-hourly --location us-east1
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
