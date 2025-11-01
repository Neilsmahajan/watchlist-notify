# syntax=docker/dockerfile:1.7

# --- Build stage ---
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install CA certificates and git (for go modules using VCS)
RUN apk add --no-cache ca-certificates git build-base

# Leverage Docker layer caching: go mod files first
COPY go.mod go.sum ./
RUN go mod download

# Install swag CLI for generating Swagger docs
RUN go install github.com/swaggo/swag/cmd/swag@latest

# Copy the rest of the source
COPY . .

# Generate Swagger documentation
RUN swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal

# Set environment for static linking and smaller binary
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

# Build the API binary
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -trimpath -ldflags="-s -w" -o /bin/api ./cmd/api


# --- Runtime stage ---
# Distroless static contains CA certs; perfect for static Go binaries
FROM gcr.io/distroless/static:nonroot

ENV PORT=8080
EXPOSE 8080

# Set working directory (optional)
WORKDIR /

# Copy binary from builder
COPY --from=builder /bin/api /api

# Run as non-root user
USER nonroot:nonroot

# Start server
ENTRYPOINT ["/api"]
