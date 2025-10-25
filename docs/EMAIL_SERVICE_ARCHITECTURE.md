# Email Digest Service - Complete Implementation Guide

## Overview

This document outlines the recommended architecture for implementing scheduled digest emails for Watchlist Notify.

## ✅ Recommended Architecture: Cloud Run + Cloud Scheduler

### Why This Approach?

1. **Serverless Benefits**: No infrastructure management, automatic scaling
2. **Cost-Effective**: Pay only for execution time (~$75/month for moderate usage)
3. **Simple to Deploy**: Leverages your existing Cloud Run knowledge
4. **Easy to Debug**: Same tooling as your API
5. **Scalable**: Handles 1,000-10,000 users easily
6. **Good Learning Path**: Can migrate to Kubernetes later without code changes

### Architecture Diagram

```
┌──────────────┐
│   Frontend   │  (Vercel)
│   Next.js    │
└──────┬───────┘
       │
       │ API calls
       ▼
┌──────────────────┐
│   Cloud Run API  │  (Your existing backend)
│   Go Backend     │
└──────┬───────────┘
       │
       │ Updates preferences
       │ Calculates next_scheduled_at
       ▼
┌──────────────────┐
│    MongoDB       │
│  User Prefs DB   │
└──────┬───────────┘
       ▲
       │ Queries users
       │ Updates timestamps
┌──────┴────────────┐
│  Cloud Run Worker │  (New digest service)
│  Digest Generator │
└──────┬────────────┘
       │
       │ Triggered hourly by
┌──────┴────────────┐
│ Cloud Scheduler   │
└───────────────────┘
       │
       │ Sends emails via
       ▼
┌───────────────────┐
│    Postmark API   │
└───────────────────┘
```

## 📁 Project Structure

```
watchlist-notify/
├── cmd/
│   ├── api/              # Existing API server
│   │   └── main.go
│   └── digest-worker/    # NEW: Digest email worker
│       └── main.go
├── internal/
│   ├── database/
│   │   ├── database.go
│   │   ├── users_repo.go       # Contains GetUsersForDigest()
│   │   └── watchlist_repo.go
│   ├── digest/           # NEW: Digest generation logic
│   │   └── generator.go        # HTML email templates, content logic
│   ├── models/
│   │   ├── users.go            # DigestSettings with next_scheduled_at
│   │   └── watchlist_items.go
│   ├── notifications/
│   │   └── sender.go           # Postmark integration
│   └── server/
│       ├── handlers_me.go      # Calculates next_scheduled_at on preference updates
│       └── helpers.go          # CalculateNextDigestTime() exported function
├── docs/
│   └── DIGEST_WORKER_DEPLOYMENT.md  # Detailed deployment guide
├── scripts/
│   ├── release.sh          # Existing API deployment
│   └── release-worker.sh   # NEW: Worker deployment
├── Dockerfile              # Existing API Dockerfile
├── Dockerfile.worker       # NEW: Worker Dockerfile
└── frontend/               # Next.js app (deployed to Vercel separately)
```

## 🚀 Implementation Steps

### Phase 1: Backend Updates (Already Done ✓)

1. ✅ Added `next_scheduled_at` to `DigestSettings`
2. ✅ Created `CalculateNextDigestTime()` helper
3. ✅ Added `GetUsersForDigest()` database method
4. ✅ Added `UpdateDigestTimestamps()` database method
5. ✅ Updated preference handler to calculate `next_scheduled_at`

### Phase 2: Worker Implementation (Completed)

1. ✅ Created `cmd/digest-worker/main.go`
2. ✅ Created `internal/digest/generator.go` for email HTML generation
3. ✅ Created `Dockerfile.worker`
4. ✅ Created deployment scripts and documentation

### Phase 3: Deployment (Next Steps)

Follow the guide in `docs/DIGEST_WORKER_DEPLOYMENT.md`:

1. **Build and push worker image**

   ```bash
   ./scripts/release-worker.sh
   ```

2. **Deploy to Cloud Run**

   ```bash
   gcloud run deploy digest-worker \
     --image us-east1-docker.pkg.dev/watchlist-notify-470822/watchlistnotify/digest-worker:latest \
     --region us-east1 \
     --set-env-vars "MONGODB_URI=...,POSTMARK_SERVER_TOKEN=..." \
     --no-allow-unauthenticated \
     --max-instances 1
   ```

3. **Create Cloud Scheduler job**

   ```bash
   gcloud scheduler jobs create http digest-worker-hourly \
     --schedule "0 * * * *" \
     --uri "https://digest-worker-xxx.run.app" \
     --http-method POST \
     --oidc-service-account-email "digest-scheduler@project.iam.gserviceaccount.com"
   ```

4. **Test**
   ```bash
   gcloud scheduler jobs run digest-worker-hourly --location us-east1
   gcloud run logs read digest-worker --region us-east1
   ```

## 💡 How It Works

### User Flow

1. User updates digest preferences in frontend (Settings page)
2. Frontend calls `PATCH /api/me/preferences`
3. Backend updates MongoDB and calculates `next_scheduled_at`
4. Returns updated user object to frontend

### Worker Flow (Runs Every Hour)

1. Cloud Scheduler triggers Cloud Run worker via HTTP POST
2. Worker queries MongoDB for users where `next_scheduled_at <= now`
3. For each eligible user:
   - Fetch their watchlist
   - Check for newly available content
   - Generate HTML email
   - Send via Postmark
   - Update `last_sent_at` and `next_scheduled_at`
4. Worker completes and shuts down until next hour

### Email Content

- **Subject**: "X items from your watchlist are now available!"
- **Body**:
  - List of newly available items with streaming services
  - Watchlist stats (total, available, watching)
  - CTA button to view full watchlist
  - Unsubscribe link in footer

## 🔄 When to Migrate to Kubernetes

**Stay with Cloud Run + Cloud Scheduler if:**

- You have < 10,000 active users
- Current costs are acceptable
- You value simplicity over control

**Migrate to Kubernetes when:**

- You have 10,000+ users and want more control
- You need parallel processing for faster batch sends
- You want to implement advanced features (timezone-aware scheduling, regional batches)
- You specifically want to learn Kubernetes/Helm
- Cost optimization becomes critical

**Kubernetes Migration Path:**

1. Your code is already Kubernetes-ready (no changes needed)
2. Create Kubernetes manifests (CronJob, Secrets, ConfigMap)
3. Deploy Helm chart
4. Same Docker images work on both platforms

Example K8s CronJob (future reference):

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: digest-worker
spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: digest-worker
              image: us-east1-docker.pkg.dev/.../digest-worker:latest
              envFrom:
                - secretRef:
                    name: digest-secrets
```

## 📊 Cost Breakdown

**Cloud Run Worker**

- Execution: ~1-5 min/hour (depends on user count)
- Memory: 512 MB
- CPU: 1 vCPU
- Cost: ~$1-3/month for 100 users, ~$10-15/month for 1,000 users

**Cloud Scheduler**

- 24 invocations/day × 30 days = 720/month
- $0.10 per job (first 3 free)
- Cost: ~$72/month

**Total: ~$75-90/month**

Compare to Kubernetes:

- GKE cluster: ~$75/month minimum (1 node)
- Worker pods: minimal additional cost
- Total: Similar cost but more complexity

## 🎯 Summary

**For your current stage (MVP with growing user base):**

✅ **Use Cloud Run + Cloud Scheduler**

- Simpler deployment
- Lower operational overhead
- Scales automatically
- Easy monitoring/debugging
- Can handle thousands of users

❌ **Don't use Kubernetes yet**

- Adds unnecessary complexity
- Requires more DevOps knowledge
- Similar cost for your scale
- Overkill for current needs

**Next Steps:**

1. Deploy the worker using `./scripts/release-worker.sh`
2. Set up Cloud Scheduler
3. Test with real users
4. Monitor logs and costs
5. Optimize HTML templates and content logic
6. Add features (unsubscribe links, A/B testing, analytics)
7. Consider Kubernetes migration when you hit 10k+ users

---

**Questions or Issues?**

- See `docs/DIGEST_WORKER_DEPLOYMENT.md` for detailed deployment steps
- Check Cloud Run logs: `gcloud run logs read digest-worker`
- Test scheduler: `gcloud scheduler jobs run digest-worker-hourly`
