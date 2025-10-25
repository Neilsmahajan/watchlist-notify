# Digest Worker Deployment Guide

## Architecture Overview

The digest worker is a **Cloud Run Job** that runs on a schedule via **Cloud Scheduler**.

```
┌─────────────────┐      Triggers every hour      ┌──────────────────┐
│ Cloud Scheduler │ ─────────────────────────────> │ Cloud Run Job    │
└─────────────────┘                                │ (digest-worker)  │
                                                   └──────────────────┘
                                                            │
                                                            │ Queries users
                                                            │ Sends emails
                                                            ▼
                                                    ┌──────────────┐
                                                    │   MongoDB    │
                                                    │   Postmark   │
                                                    └──────────────┘
```

**Cloud Run Jobs vs Services:**

- **Jobs** = Batch workloads that run to completion and exit (our digest worker)
- **Services** = Always-on HTTP servers that handle requests (our API)

## Deployment Steps

### 1. Build and Push Worker Image

```bash
# Set your project variables (or source from .deploy.env)
export PROJECT_ID="watchlist-notify-470822"
export REGION="us-east1"
export REPO="watchlistnotify"

# Build the worker image
docker buildx build --platform linux/amd64 \
  -f Dockerfile.worker \
  -t ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/digest-worker:latest \
  .

# Push to Artifact Registry
docker push ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/digest-worker:latest
```

### 2. Deploy Worker as Cloud Run Job

```bash
# Deploy the worker job (runs to completion, doesn't listen on HTTP)
gcloud run jobs create digest-worker \
  --image ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/digest-worker:latest \
  --region ${REGION} \
  --set-env-vars "MONGODB_URI=${MONGODB_URI},POSTMARK_SERVER_TOKEN=${POSTMARK_SERVER_TOKEN},POSTMARK_BASE_URL=https://api.postmarkapp.com/email" \
  --memory 512Mi \
  --task-timeout 540s \
  --max-retries 1
```

**Important flags explained:**

- `--task-timeout 540s`: 9 minutes max per execution
- `--max-retries 1`: Retry once if job fails
- `--memory 512Mi`: Allocate 512MB RAM

### 3. Create Cloud Scheduler Job

```bash
# Create a service account for the scheduler (if not exists)
gcloud iam service-accounts create digest-scheduler \
  --display-name "Digest Worker Scheduler"

# Grant the service account permission to execute Cloud Run Jobs
gcloud run jobs add-iam-policy-binding digest-worker \
  --region ${REGION} \
  --member "serviceAccount:digest-scheduler@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role "roles/run.invoker"

# Create the scheduler job (runs every hour at :00)
gcloud scheduler jobs create http digest-worker-hourly \
  --location ${REGION} \
  --schedule "0 * * * *" \
  --uri "https://${REGION}-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/${PROJECT_ID}/jobs/digest-worker:run" \
  --http-method POST \
  --oauth-service-account-email "digest-scheduler@${PROJECT_ID}.iam.gserviceaccount.com" \
  --description "Runs digest email worker every hour"
```

### 4. Test the Deployment

```bash
# Manually execute the job
gcloud run jobs execute digest-worker --region ${REGION}

# View execution logs
gcloud logging read "resource.type=cloud_run_job AND resource.labels.job_name=digest-worker" \
  --limit 50 \
  --format "table(timestamp,severity,textPayload)"

# Or manually trigger the scheduler job
gcloud scheduler jobs run digest-worker-hourly --location ${REGION}
```

## Deployment Steps

### 1. Build and Push Worker Image

```bash
# Set your project variables (or source from .deploy.env)
export PROJECT_ID="watchlist-notify-470822"
export REGION="us-east1"
export REPO="watchlistnotify"

# Build the worker image
docker buildx build --platform linux/amd64 \
  -f Dockerfile.worker \
  -t ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/digest-worker:latest \
  .

# Push to Artifact Registry
docker push ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/digest-worker:latest
```

### 2. Deploy Worker as Cloud Run Service

```bash
# Deploy the worker (will not accept HTTP traffic, only Cloud Scheduler invocations)
gcloud run deploy digest-worker \
  --image ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/digest-worker:latest \
  --region ${REGION} \
  --platform managed \
  --no-allow-unauthenticated \
  --set-env-vars "MONGODB_URI=${MONGODB_URI},POSTMARK_SERVER_TOKEN=${POSTMARK_SERVER_TOKEN},POSTMARK_BASE_URL=https://api.postmarkapp.com/email" \
  --memory 512Mi \
  --timeout 540s \
  --max-instances 1 \
  --concurrency 1 \
  --no-cpu-throttling
```

**Important flags explained:**

- `--no-allow-unauthenticated`: Only Cloud Scheduler can invoke it
- `--max-instances 1`: Prevent concurrent runs (digest processing should be serial)
- `--concurrency 1`: One request at a time
- `--timeout 540s`: 9 minutes max (Cloud Run allows up to 60min for jobs)
- `--no-cpu-throttling`: Keep CPU allocated even when idle (faster startup)

### 3. Create Cloud Scheduler Job

```bash
# Get the worker service URL
WORKER_URL=$(gcloud run services describe digest-worker \
  --region ${REGION} \
  --format 'value(status.url)')

# Create a service account for the scheduler (if not exists)
gcloud iam service-accounts create digest-scheduler \
  --display-name "Digest Worker Scheduler"

# Grant the service account permission to invoke Cloud Run
gcloud run services add-iam-policy-binding digest-worker \
  --region ${REGION} \
  --member "serviceAccount:digest-scheduler@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role "roles/run.invoker"

# Create the scheduler job (runs every hour at :00)
gcloud scheduler jobs create http digest-worker-hourly \
  --location ${REGION} \
  --schedule "0 * * * *" \
  --uri "${WORKER_URL}" \
  --http-method POST \
  --oidc-service-account-email "digest-scheduler@${PROJECT_ID}.iam.gserviceaccount.com" \
  --oidc-token-audience "${WORKER_URL}" \
  --attempt-deadline 540s \
  --description "Runs digest email worker every hour"
```

### 4. Test the Deployment

```bash
# Manually trigger the scheduler job
gcloud scheduler jobs run digest-worker-hourly --location ${REGION}

# View logs
gcloud run logs read digest-worker --region ${REGION} --limit 50
```

## Environment Variables Needed

The worker requires the same environment variables as your API:

```bash
MONGODB_URI=mongodb+srv://...
POSTMARK_SERVER_TOKEN=your-token
POSTMARK_BASE_URL=https://api.postmarkapp.com/email
```

## Updating the Worker

```bash
# Build new version
docker buildx build --platform linux/amd64 \
  -f Dockerfile.worker \
  -t ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/digest-worker:latest \
  .

# Push
docker push ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/digest-worker:latest

# Deploy update
gcloud run deploy digest-worker \
  --image ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/digest-worker:latest \
  --region ${REGION}
```

## Monitoring

```bash
# View recent runs
gcloud scheduler jobs describe digest-worker-hourly --location ${REGION}

# View execution history
gcloud logging read "resource.type=cloud_run_revision AND resource.labels.service_name=digest-worker" \
  --limit 50 \
  --format json

# Check for errors
gcloud logging read "resource.type=cloud_run_revision AND resource.labels.service_name=digest-worker AND severity>=ERROR" \
  --limit 20
```

## Cost Estimation

**Cloud Run Worker:**

- Runs ~1-5 minutes per hour (depends on user count)
- 512 MB RAM, 1 vCPU
- Estimated: ~$1-3/month for 100 users

**Cloud Scheduler:**

- 24 jobs/day × 30 days = 720 jobs/month
- First 3 jobs/month are free
- $0.10 per job beyond that
- Estimated: ~$72/month

**Total: ~$75/month**

## Scaling Considerations

This architecture handles up to ~10,000 users easily:

- Each digest send takes ~100-200ms
- 10,000 users = ~20 minutes of execution
- Well within Cloud Run's 60-minute timeout

Beyond 10,000 users, consider:

- Batching users across multiple scheduler triggers
- Using Cloud Run Jobs instead of Cloud Run services
- Migrating to Kubernetes for more control

---

## Alternative: When to Use Kubernetes

**Migrate to Kubernetes when:**

- You have 10,000+ active users
- You need more control over scheduling (e.g., regional batches, time zones)
- You want to learn Kubernetes/Helm
- You need to run multiple workers in parallel
- Cost optimization becomes important at scale

**Kubernetes Setup (Brief Overview):**

```yaml
# k8s/cronjob.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: digest-worker
spec:
  schedule: "0 * * * *" # Every hour
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: digest-worker
              image: us-east1-docker.pkg.dev/watchlist-notify-470822/watchlistnotify/digest-worker:latest
              env:
                - name: MONGODB_URI
                  valueFrom:
                    secretKeyRef:
                      name: watchlist-secrets
                      key: mongodb-uri
                - name: POSTMARK_SERVER_TOKEN
                  valueFrom:
                    secretKeyRef:
                      name: watchlist-secrets
                      key: postmark-token
          restartPolicy: OnFailure
```

**But for now, stick with Cloud Run + Cloud Scheduler!**
