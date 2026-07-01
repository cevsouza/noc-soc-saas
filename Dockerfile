# Dockerfile
# Multi-stage and multi-language build for Go and Python SRE environment

# 1. Build Go binary
FROM golang:1.21-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags="-w -s" -o out ./cmd/noc-api

# 2. Production container containing Go runtime and Python SRE runbooks
FROM python:3.11-alpine
WORKDIR /app

# Install system dependencies (SSH, certificates, etc.)
RUN apk add --no-cache ca-certificates openssh-client

# Install Python requirements
COPY workers/requirements.txt ./workers/requirements.txt
RUN pip install --no-cache-dir -r ./workers/requirements.txt

# Copy Go binary, playbooks and resources
COPY --from=go-builder /app/out ./out
COPY --from=go-builder /app/workers ./workers
COPY --from=go-builder /app/scripts ./scripts
COPY --from=go-builder /app/internal/db/migrations ./internal/db/migrations
COPY --from=go-builder /app/frontend ./frontend

# Expose port and start
EXPOSE 8080
CMD ["./out"]
