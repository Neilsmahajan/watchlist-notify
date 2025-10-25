#!/usr/bin/env bash
# release-worker.sh - Build, push, and deploy the digest worker to Cloud Run
#
# Usage:
#   scripts/release-worker.sh [--tag TAG] [--cloud-build] [--no-deploy] [--env "K=V"] [--env-file FILE] [--dry-run]
#
# Examples:
#   scripts/release-worker.sh                    # build locally, push, deploy
#   scripts/release-worker.sh --cloud-build      # build via Cloud Build
#   scripts/release-worker.sh --tag v1.2.3       # custom tag
#   scripts/release-worker.sh --no-deploy        # just build and push
#   scripts/release-worker.sh --env-file cloudrun.env  # deploy with env vars from file
#   scripts/release-worker.sh --env "FOO=bar,HELLO=world"  # set specific env vars
#   scripts/release-worker.sh --dry-run          # show what would happen

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${ROOT_DIR}" || exit 1

# --- Helper functions -------------------------------------------------------
color() {
  case "$1" in
    red)    echo -ne "\033[0;31m" ;;
    green)  echo -ne "\033[0;32m" ;;
    yellow) echo -ne "\033[0;33m" ;;
    cyan)   echo -ne "\033[0;36m" ;;
    *)      echo -ne "\033[0m" ;;
  esac
  echo -n "$2"
  echo -e "\033[0m"
}

log() { color cyan "[worker-release]"; echo " $*"; }
warn() { color yellow "[warn]"; echo " $*"; }
err()  { color red "[error]"; echo " $*" >&2; }

# --- Load configuration -----------------------------------------------------
if [[ -f .deploy.env ]]; then
  log "Loading .deploy.env"
  export $(grep -v '^#' .deploy.env | grep -E '^[A-Za-z_][A-Za-z0-9_]*=' | xargs -0 2>/dev/null || true)
fi

PROJECT_ID="${PROJECT_ID:-}"
REGION="${REGION:-}"
REPO="${REPO:-}"
PLATFORM="${PLATFORM:-linux/amd64}"

# Worker-specific defaults
WORKER_IMAGE="${WORKER_IMAGE:-digest-worker}"
WORKER_SERVICE="${WORKER_SERVICE:-digest-worker}"

# --- Defaults / state -------------------------------------------------------
DATE_UTC=$(date -u +%Y%m%d-%H%M%S)
SHORT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo "nosha")
AUTO_TAG="${DATE_UTC}-${SHORT_SHA}"
TAG=""
USE_CLOUD_BUILD=0
DO_DEPLOY=1
DRY_RUN=0
ENV_INLINE=""
ENV_FILE=""

# --- Parse args -------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag) TAG="$2"; shift 2 ;;
    --cloud-build) USE_CLOUD_BUILD=1; shift ;;
    --no-deploy) DO_DEPLOY=0; shift ;;
    --env) ENV_INLINE="$2"; shift 2 ;;
    --env-file) ENV_FILE="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --help) cat <<'HELP'
Usage: scripts/release-worker.sh [options]

Options:
  --tag TAG             Use custom tag (default: auto-generated from date+sha)
  --cloud-build         Build using Cloud Build instead of local buildx
  --no-deploy           Skip deployment (just build and push)
  --env "K=V,K2=V2"     Set/override env vars on deploy (comma separated)
  --env-file <path>     File with KEY=VAL lines to set on deploy
  --dry-run             Show what would happen without making changes
  --help                Show this help

Environment variables (from .deploy.env):
  PROJECT_ID, REGION, REPO, PLATFORM

Examples:
  scripts/release-worker.sh --env-file cloudrun.env
  scripts/release-worker.sh --env "MONGODB_URI=...,POSTMARK_SERVER_TOKEN=..."
  scripts/release-worker.sh --cloud-build --env-file cloudrun.env
HELP
    exit 0 ;;
    *) err "Unknown option: $1"; exit 1 ;;
  esac
done

# --- Validation -------------------------------------------------------------
[ -z "${PROJECT_ID}" ] && { err "PROJECT_ID required"; exit 1; }
[ -z "${REGION}" ] && { err "REGION required"; exit 1; }
[ -z "${REPO}" ] && { err "REPO required"; exit 1; }

if [[ -z ${TAG} ]]; then
  TAG="${AUTO_TAG}"
fi

REGISTRY="${REGION}-docker.pkg.dev"
IMAGE_PATH="${REGISTRY}/${PROJECT_ID}/${REPO}/${WORKER_IMAGE}"
FULL_IMAGE="${IMAGE_PATH}:${TAG}"

log "Project      : ${PROJECT_ID}"
log "Region       : ${REGION}"
log "Repository   : ${REPO}"
log "Worker Image : ${WORKER_IMAGE}"
log "Service Name : ${WORKER_SERVICE}"
log "Tag          : ${TAG}"
log "Platform     : ${PLATFORM}"
log "Cloud Build  : $([[ ${USE_CLOUD_BUILD} -eq 1 ]] && echo yes || echo no)"
[[ ${DRY_RUN} -eq 1 ]] && warn "DRY RUN MODE - no changes will be made"

# --- Build & Push -----------------------------------------------------------
if [[ ${USE_CLOUD_BUILD} -eq 1 ]]; then
  log "Building worker via Cloud Build..."
  if [[ ${DRY_RUN} -eq 0 ]]; then
    gcloud builds submit \
      --project "${PROJECT_ID}" \
      --region "${REGION}" \
      --config - \
      --substitutions "_IMAGE_PATH=${IMAGE_PATH},_TAG=${TAG}" \
      . <<'EOF'
steps:
  - name: 'gcr.io/cloud-builders/docker'
    args:
      - 'build'
      - '-f'
      - 'Dockerfile.worker'
      - '-t'
      - '${_IMAGE_PATH}:${_TAG}'
      - '.'
images:
  - '${_IMAGE_PATH}:${_TAG}'
options:
  machineType: 'E2_HIGHCPU_8'
  logging: CLOUD_LOGGING_ONLY
EOF
  else
    log "[DRY RUN] Would submit Cloud Build for ${FULL_IMAGE}"
  fi
else
  log "Building worker locally with buildx..."
  if [[ ${DRY_RUN} -eq 0 ]]; then
    docker buildx build \
      --platform "${PLATFORM}" \
      -f Dockerfile.worker \
      -t "${FULL_IMAGE}" \
      --push \
      .
  else
    log "[DRY RUN] Would run: docker buildx build -f Dockerfile.worker -t ${FULL_IMAGE} --push ."
  fi
fi

color green "✓ Worker image built and pushed: ${FULL_IMAGE}"

# --- Prepare deploy env vars ------------------------------------------------
SET_ENV_ARGS=()
MERGED_ENV=""
if [[ -n ${ENV_INLINE} ]]; then
  MERGED_ENV="${ENV_INLINE}"
fi
if [[ -n ${ENV_FILE} ]]; then
  if [[ ! -f ${ENV_FILE} ]]; then err "Env file not found: ${ENV_FILE}"; exit 1; fi
  FILE_VARS=$(grep -v '^#' "${ENV_FILE}" | grep -E '^[A-Za-z_][A-Za-z0-9_]*=' | paste -sd, -)
  if [[ -n ${MERGED_ENV} && -n ${FILE_VARS} ]]; then
    MERGED_ENV="${MERGED_ENV},${FILE_VARS}"
  elif [[ -z ${MERGED_ENV} ]]; then
    MERGED_ENV="${FILE_VARS}"
  fi
fi
if [[ -n ${MERGED_ENV} ]]; then
  SET_ENV_ARGS+=(--set-env-vars "${MERGED_ENV}")
  log "Setting environment variables from provided config"
fi

# --- Deploy -----------------------------------------------------------------
if [[ ${DO_DEPLOY} -eq 1 ]]; then
  log "Deploying worker to Cloud Run Jobs..."
  
  # Check if job exists
  if gcloud run jobs describe "${WORKER_SERVICE}" --region "${REGION}" --project "${PROJECT_ID}" &>/dev/null 2>&1; then
    log "Updating existing job..."
    if [[ ${DRY_RUN} -eq 0 ]]; then
      CMD=(gcloud run jobs update "${WORKER_SERVICE}" \
        --image "${FULL_IMAGE}" \
        --region "${REGION}" \
        --project "${PROJECT_ID}" \
        --max-retries 1 \
        --task-timeout 540s)
      if [[ ${#SET_ENV_ARGS[@]} -gt 0 ]]; then
        CMD+=("${SET_ENV_ARGS[@]}")
      fi
      "${CMD[@]}"
    else
      log "[DRY RUN] Would update job ${WORKER_SERVICE} with image ${FULL_IMAGE}"
      if [[ ${#SET_ENV_ARGS[@]} -gt 0 ]]; then
        log "[DRY RUN] Would set env vars: ${MERGED_ENV}"
      fi
    fi
  else
    log "Creating new job..."
    
    if [[ ${DRY_RUN} -eq 0 ]]; then
      CMD=(gcloud run jobs create "${WORKER_SERVICE}" \
        --image "${FULL_IMAGE}" \
        --region "${REGION}" \
        --project "${PROJECT_ID}" \
        --max-retries 1 \
        --task-timeout 540s \
        --memory 512Mi)
      if [[ ${#SET_ENV_ARGS[@]} -gt 0 ]]; then
        CMD+=("${SET_ENV_ARGS[@]}")
      fi
      "${CMD[@]}"
      
      log "Job created. Next steps:"
      if [[ ${#SET_ENV_ARGS[@]} -eq 0 ]]; then
        warn "NOTE: No environment variables were set. You may need to set:"
        warn "  - MONGODB_URI"
        warn "  - POSTMARK_SERVER_TOKEN"
        warn "  - POSTMARK_BASE_URL"
      fi
      log "Create Cloud Scheduler job (see docs/DIGEST_WORKER_DEPLOYMENT.md)"
    else
      log "[DRY RUN] Would create new job ${WORKER_SERVICE}"
      if [[ ${#SET_ENV_ARGS[@]} -gt 0 ]]; then
        log "[DRY RUN] Would set env vars: ${MERGED_ENV}"
      fi
    fi
  fi
  
  color green "✓ Worker job deployed successfully"
else
  log "Skipping deployment (--no-deploy)"
fi

color green "✓ Worker release complete!"
log "Image: ${FULL_IMAGE}"
log "Job: ${WORKER_SERVICE}"
