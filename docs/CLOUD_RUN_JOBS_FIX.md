# Cloud Run Jobs vs Services - Quick Fix Guide

## Problem

When deploying the digest worker, you encountered this error:

```
The user-provided container failed to start and listen on the port defined
provided by the PORT=8080 environment variable within the allocated timeout.
```

## Root Cause

The digest worker is a **batch job** that:

- Runs to completion
- Processes users and sends emails
- Exits when done

Cloud Run **Services** expect containers to:

- Start an HTTP server
- Listen on port 8080
- Keep running indefinitely

This mismatch caused the deployment failure.

## Solution

Use **Cloud Run Jobs** instead of Cloud Run Services.

### Cloud Run Jobs vs Services

| Feature          | Cloud Run Jobs                 | Cloud Run Services |
| ---------------- | ------------------------------ | ------------------ |
| **Purpose**      | Batch/scheduled tasks          | HTTP servers       |
| **Runs**         | To completion, then exits      | Continuously       |
| **Triggered by** | Manual, Scheduler, Events      | HTTP requests      |
| **Port 8080**    | Not required                   | Required           |
| **Use cases**    | Digest emails, data processing | APIs, webhooks     |

## Fixed Commands

### Old (Failed) - Service

```bash
gcloud run deploy digest-worker \
  --image ... \
  --no-allow-unauthenticated \
  --max-instances 1
```

### New (Working) - Job

```bash
gcloud run jobs create digest-worker \
  --image ... \
  --max-retries 1 \
  --task-timeout 540s
```

## Updated Deployment

Run this to deploy correctly:

```bash
./scripts/release-worker.sh --env-file cloudrun.env
```

The script now:

1. ✅ Uses `gcloud run jobs create/update` instead of `gcloud run deploy`
2. ✅ No need for HTTP port configuration
3. ✅ Proper timeout and retry settings for batch jobs

## Cloud Scheduler Integration

Cloud Scheduler can trigger both Services and Jobs, but the URI format differs:

### For Jobs (New Way)

```bash
--uri "https://us-east1-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/PROJECT_ID/jobs/digest-worker:run"
--oauth-service-account-email "..."
```

### For Services (Old Way)

```bash
--uri "https://digest-worker-xxx.run.app"
--oidc-service-account-email "..."
```

## Testing

```bash
# Execute job manually
gcloud run jobs execute digest-worker --region us-east1

# View execution history
gcloud run jobs executions list --job digest-worker --region us-east1

# View logs
gcloud logging read "resource.type=cloud_run_job AND resource.labels.job_name=digest-worker" --limit 50
```

## Summary

- **Before**: Trying to deploy batch job as a service → Failed (no HTTP server)
- **After**: Deploy as Cloud Run Job → Works correctly ✅
- **Cost**: Same (~$75/month)
- **Functionality**: Identical, just the right deployment type
