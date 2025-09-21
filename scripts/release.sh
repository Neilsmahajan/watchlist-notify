#!/usr/bin/env bash
# release.sh - Build, push, and optionally deploy the Go API to Cloud Run.
#
# Features:
#  * Auto tag (UTC date + short git sha) or custom --tag
#  * Local buildx (Apple Silicon cross-build) OR Cloud Build (--cloud-build)
#  * Optional deploy step (default on). Use --no-deploy to skip
#  * Deploy-only mode to redeploy an existing tag without rebuilding
#  * Optional tagging of :latest
#  * Optional env var injection on deploy (--env or --env-file) for first-time or updates
#  * Dry-run mode to show what would happen
#
# Usage examples:
#   scripts/release.sh                    # build (local), push, deploy with auto tag
#   scripts/release.sh --tag my-test       # custom tag
#   scripts/release.sh --cloud-build       # build via Cloud Build (faster / native amd64)
#   scripts/release.sh --no-deploy         # just build + push
#   scripts/release.sh --deploy-only --tag 20250921-120000-a1b2c3d  # re-deploy existing image
#   scripts/release.sh --tag feat-x --latest                       # also push :latest
#   scripts/release.sh --env "FOO=bar,HELLO=world"                # inject/overwrite env vars
#   scripts/release.sh --env-file deploy.env                       # load KEY=VAL lines
#   scripts/release.sh --dry-run
#
# Pre-reqs:
#   - gcloud CLI authenticated (gcloud auth login && gcloud config set project ...)
#   - Artifact Registry repo exists
#   - .deploy.env (or pass vars via env) containing PROJECT_ID, REGION, REPO, IMAGE, SERVICE
#
# .deploy.env example (already provided as .deploy.env.example):
#   PROJECT_ID=watchlist-notify-470822
#   REGION=us-east1
#   REPO=watchlistnotify
#   IMAGE=api
#   SERVICE=watchlist-api
#   PLATFORM=linux/amd64
#
# Exit codes:
#   0 success
#   1 usage / validation error
#   2 build failure
#   3 deploy failure

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${ROOT_DIR}" || exit 1

# --- Helper functions -------------------------------------------------------
color() { # $1=colorName $2=message
  local c="" r="\033[0m"
  case "$1" in
    red) c="\033[31m";; green) c="\033[32m";; yellow) c="\033[33m";; blue) c="\033[34m";; magenta) c="\033[35m";; cyan) c="\033[36m";; *) c="";;
  esac
  printf "%b%s%b" "$c" "$2" "$r"
}

log() { color cyan "[release]"; echo " $*"; }
warn() { color yellow "[warn]"; echo " $*"; }
err()  { color red "[error]"; echo " $*" >&2; }

usage() {
  cat <<'USAGE'
Usage: scripts/release.sh [options]

Options:
  --tag <tag>          Use custom tag instead of auto (date+sha)
  --cloud-build        Use Cloud Build instead of local docker buildx
  --no-deploy          Build & push only (skip deploy)
  --deploy-only        Skip build/push; deploy existing tag (requires --tag)
  --latest             Also tag/push image as :latest
  --env "K=V,K2=V2"    Set/override env vars on deploy (comma separated)
  --env-file <path>    File with KEY=VAL lines to set on deploy
  --dry-run            Show actions without executing
  --help               Show this help

Environment / .deploy.env required values:
  PROJECT_ID, REGION, REPO, IMAGE, SERVICE (and optional PLATFORM=linux/amd64)

Examples:
  scripts/release.sh --cloud-build --latest
  scripts/release.sh --tag hotfix-123 --env "APP_ENV=production" --latest
  scripts/release.sh --deploy-only --tag 20250921-120000-a1b2c3d
USAGE
}

# --- Load configuration -----------------------------------------------------
if [[ -f .deploy.env ]]; then
  # shellcheck disable=SC2046
  export $(grep -v '^#' .deploy.env | grep -E '^[A-Za-z_][A-Za-z0-9_]*=' | xargs -0 2>/dev/null || true)
fi

PROJECT_ID="${PROJECT_ID:-}"
REGION="${REGION:-}"
REPO="${REPO:-}"
IMAGE="${IMAGE:-}"
SERVICE="${SERVICE:-}"
PLATFORM="${PLATFORM:-linux/amd64}"

# --- Defaults / state -------------------------------------------------------
DATE_UTC=$(date -u +%Y%m%d-%H%M%S)
SHORT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo "nosha")
AUTO_TAG="${DATE_UTC}-${SHORT_SHA}"
TAG=""
USE_CLOUD_BUILD=0
DO_DEPLOY=1
DEPLOY_ONLY=0
TAG_LATEST=0
DRY_RUN=0
ENV_INLINE=""
ENV_FILE=""

# --- Parse args -------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag) TAG="$2"; shift 2;;
    --cloud-build) USE_CLOUD_BUILD=1; shift;;
    --no-deploy) DO_DEPLOY=0; shift;;
    --deploy-only) DEPLOY_ONLY=1; shift;;
    --latest) TAG_LATEST=1; shift;;
    --env) ENV_INLINE="$2"; shift 2;;
    --env-file) ENV_FILE="$2"; shift 2;;
    --dry-run) DRY_RUN=1; shift;;
    --help|-h) usage; exit 0;;
    *) err "Unknown argument: $1"; usage; exit 1;;
  esac
done

# --- Validation -------------------------------------------------------------
[ -z "${PROJECT_ID}" ] && { err "PROJECT_ID required"; exit 1; }
[ -z "${REGION}" ] && { err "REGION required"; exit 1; }
[ -z "${REPO}" ] && { err "REPO required"; exit 1; }
[ -z "${IMAGE}" ] && { err "IMAGE required"; exit 1; }
[ -z "${SERVICE}" ] && { err "SERVICE required"; exit 1; }

if [[ ${DEPLOY_ONLY} -eq 1 && -z ${TAG} ]]; then
  err "--deploy-only requires --tag <tag>"
  exit 1
fi

if [[ -z ${TAG} ]]; then
  TAG="${AUTO_TAG}"
fi

REGISTRY="${REGION}-docker.pkg.dev"
IMAGE_PATH="${REGISTRY}/${PROJECT_ID}/${REPO}/${IMAGE}"
FULL_IMAGE="${IMAGE_PATH}:${TAG}"
LATEST_IMAGE="${IMAGE_PATH}:latest"

log "Project      : ${PROJECT_ID}"
log "Region       : ${REGION}"
log "Repository   : ${REPO}"
log "Image name   : ${IMAGE}"
log "Service      : ${SERVICE}"
log "Tag          : ${TAG}${DEPLOY_ONLY:+ (deploy-only)}"
log "Platform     : ${PLATFORM}"
log "Cloud Build  : $([[ ${USE_CLOUD_BUILD} -eq 1 ]] && echo yes || echo no)"
log "Also latest  : $([[ ${TAG_LATEST} -eq 1 ]] && echo yes || echo no)"
[[ ${DRY_RUN} -eq 1 ]] && warn "DRY RUN MODE - no changes will be made"

# --- Build & Push -----------------------------------------------------------
if [[ ${DEPLOY_ONLY} -eq 0 ]]; then
  if [[ ${USE_CLOUD_BUILD} -eq 1 ]]; then
    log "Building with Cloud Build: ${FULL_IMAGE}"
    CMD=(gcloud builds submit --region "${REGION}" --tag "${FULL_IMAGE}" .)
    if [[ ${DRY_RUN} -eq 1 ]]; then echo "DRY: ${CMD[*]}"; else
      if ! "${CMD[@]}"; then err "Cloud Build failed"; exit 2; fi
    fi
    if [[ ${TAG_LATEST} -eq 1 ]]; then
      CMD=(gcloud artifacts docker tags add "${FULL_IMAGE}" "${LATEST_IMAGE}")
      if [[ ${DRY_RUN} -eq 1 ]]; then echo "DRY: ${CMD[*]}"; else "${CMD[@]}" || warn "Failed to add latest tag"; fi
    fi
  else
    log "Ensuring docker auth for ${REGISTRY}"
    CMD=(gcloud auth configure-docker "${REGISTRY}")
    if [[ ${DRY_RUN} -eq 1 ]]; then echo "DRY: ${CMD[*]}"; else "${CMD[@]}" >/dev/null; fi

    log "Local buildx build: ${FULL_IMAGE}"
    CMD=(docker buildx build --platform "${PLATFORM}" -t "${FULL_IMAGE}" .)
    if [[ ${DRY_RUN} -eq 1 ]]; then echo "DRY: ${CMD[*]}"; else
      if ! "${CMD[@]}"; then err "Local docker build failed"; exit 2; fi
    fi

    log "Pushing image: ${FULL_IMAGE}"
    CMD=(docker push "${FULL_IMAGE}")
    if [[ ${DRY_RUN} -eq 1 ]]; then echo "DRY: ${CMD[*]}"; else
      if ! "${CMD[@]}"; then err "Push failed"; exit 2; fi
    fi

    if [[ ${TAG_LATEST} -eq 1 ]]; then
      log "Tagging latest"
      CMD=(docker tag "${FULL_IMAGE}" "${LATEST_IMAGE}")
      if [[ ${DRY_RUN} -eq 1 ]]; then echo "DRY: ${CMD[*]}"; else "${CMD[@]}"; fi
      CMD=(docker push "${LATEST_IMAGE}")
      if [[ ${DRY_RUN} -eq 1 ]]; then echo "DRY: ${CMD[*]}"; else "${CMD[@]}" || warn "Failed to push latest"; fi
    fi
  fi
else
  log "Skipping build/push (--deploy-only). Assuming image exists: ${FULL_IMAGE}"
fi

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
fi

# --- Deploy -----------------------------------------------------------------
if [[ ${DO_DEPLOY} -eq 1 ]]; then
  log "Deploying revision with image ${FULL_IMAGE}"
  CMD=(gcloud run deploy "${SERVICE}" --image "${FULL_IMAGE}" --region "${REGION}" --platform managed --quiet)
  if [[ ${#SET_ENV_ARGS[@]} -gt 0 ]]; then
    CMD+=("${SET_ENV_ARGS[@]}")
  fi
  if [[ ${DRY_RUN} -eq 1 ]]; then echo "DRY: ${CMD[*]}"; else
    if ! "${CMD[@]}"; then err "Deploy failed"; exit 3; fi
  fi
  if [[ ${DRY_RUN} -eq 0 ]]; then
    URL=$(gcloud run services describe "${SERVICE}" --region "${REGION}" --format 'value(status.url)') || URL="(unknown)"
    log "Service URL: ${URL}"
  fi
else
  log "Skipping deploy (--no-deploy)"
fi

log "Done. Image: ${FULL_IMAGE}"
if [[ ${TAG_LATEST} -eq 1 ]]; then log "Also tagged: ${LATEST_IMAGE}"; fi

