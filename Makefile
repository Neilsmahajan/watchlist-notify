# Simple Makefile for a Go project

# -----------------------------
# Docker/Cloud Run configuration
# -----------------------------
# Set these via environment or edit defaults below
PROJECT_ID ?=
REGION ?= us-central1
REPO ?= watchlist
IMAGE ?= api
SERVICE ?= watchlist-api
PLATFORM ?= linux/amd64

# Allow loading a local deployment env file (not committed) for convenience
# Create a .deploy.env with PROJECT_ID=..., REGION=..., etc.
ifneq (,$(wildcard .deploy.env))
include .deploy.env
export
endif

REGISTRY := $(REGION)-docker.pkg.dev
IMAGE_URI := $(REGISTRY)/$(PROJECT_ID)/$(REPO)/$(IMAGE)

# Original TAG fallback (git short sha). We introduce a richer RELEASE_TAG (date+sha) for release target.
TAG ?= $(shell git rev-parse --short HEAD)
DATE_UTC := $(shell date -u +%Y%m%d-%H%M%S)
SHORT_SHA := $(shell git rev-parse --short HEAD)
RELEASE_TAG := $(DATE_UTC)-$(SHORT_SHA)

# Build the application
all: build test

build:
	@echo "Building..."
	@go build -o main cmd/api/main.go

# Run the application
run:
	@go run cmd/api/main.go

# Create DB container
docker-run:
	@if docker compose up --build 2>/dev/null; then \
		: ; \
	else \
		echo "Falling back to Docker Compose V1"; \
		docker-compose up --build; \
	fi

# Shutdown DB container
docker-down:
	@if docker compose down 2>/dev/null; then \
		: ; \
	else \
		echo "Falling back to Docker Compose V1"; \
		docker-compose down; \
	fi

# Test the application
test:
	@echo "Testing..."
	@go test ./... -v

# Integrations Tests for the application
itest:
	@echo "Running integration tests..."
	@go test ./internal/database -v

# Clean the binary
clean:
	@echo "Cleaning..."
	@rm -f main

# Live Reload
watch:
	@if command -v air > /dev/null; then \
            air; \
            echo "Watching...";\
        else \
            read -p "Go's 'air' is not installed on your machine. Do you want to install it? [Y/n] " choice; \
            if [ "$$choice" != "n" ] && [ "$$choice" != "N" ]; then \
                go install github.com/air-verse/air@latest; \
                air; \
                echo "Watching...";\
            else \
                echo "You chose not to install air. Exiting..."; \
                exit 1; \
            fi; \
        fi

# -----------------------------
# Docker / Cloud Run targets
# -----------------------------

# Ensure Docker auth to Artifact Registry
docker-auth:
	@if [ -z "$(PROJECT_ID)" ]; then echo "PROJECT_ID is required"; exit 1; fi
	gcloud auth configure-docker $(REGISTRY)

# Build linux image on Apple Silicon (or any host) using buildx
docker-build:
	@if [ -z "$(PROJECT_ID)" ]; then echo "PROJECT_ID is required"; exit 1; fi
	docker buildx build \
		--platform $(PLATFORM) \
		-t $(IMAGE_URI):$(TAG) \
		.

# Push built image
docker-push:
	@if [ -z "$(PROJECT_ID)" ]; then echo "PROJECT_ID is required"; exit 1; fi
	docker push $(IMAGE_URI):$(TAG)

# Convenience: build and push in one step (recommended on Apple Silicon)
docker-build-push:
	@if [ -z "$(PROJECT_ID)" ]; then echo "PROJECT_ID is required"; exit 1; fi
	docker buildx build \
		--platform $(PLATFORM) \
		-t $(IMAGE_URI):$(TAG) \
		--push \
		.

# Run container locally
run-container:
	@if [ -z "$(PROJECT_ID)" ]; then echo "PROJECT_ID is required"; exit 1; fi
	docker run --rm -p 8080:8080 \
		-e PORT=8080 \
		-e MONGODB_URI="$$MONGODB_URI" \
		-e JWT_SECRET="$$JWT_SECRET" \
		-e TMDB_API_KEY="$$TMDB_API_KEY" \
		$(IMAGE_URI):$(TAG)

# Deploy to Cloud Run (env vars assumed already configured from initial deploy in console/CLI)
deploy:
	@if [ -z "$(PROJECT_ID)" ]; then echo "PROJECT_ID is required"; exit 1; fi
	gcloud run deploy $(SERVICE) \
		--image $(IMAGE_URI):$(TAG) \
		--region $(REGION) \
		--platform managed \
		--allow-unauthenticated \
		--port 8080 \
		--memory 512Mi \
		--max-instances 3 \
		--min-instances 0

# Show the fully qualified image reference
show-image:
	@if [ -z "$(PROJECT_ID)" ]; then echo "PROJECT_ID is required"; exit 1; fi
	@echo $(IMAGE_URI):$(TAG)

# One-shot release: auto date+sha tag build+push+deploy
release:
	@if [ -z "$(PROJECT_ID)" ]; then echo "PROJECT_ID is required"; exit 1; fi
	@echo "[release] Using tag $(RELEASE_TAG)"
	$(MAKE) TAG=$(RELEASE_TAG) docker-build-push
	$(MAKE) TAG=$(RELEASE_TAG) deploy
	@echo "[release] Deployed image: $(IMAGE_URI):$(RELEASE_TAG)"

.PHONY: all build run test clean watch docker-run docker-down itest docker-auth docker-build docker-push docker-build-push run-container deploy show-image release
