# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make protobuf protobuf-dev

# Set working directory
WORKDIR /app

# Copy source code (needed for local module dependencies)
COPY . .

# Download dependencies (after copying source to resolve local replaces)
RUN go mod download

# Install protobuf Go plugins
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate protobuf code and build CLI only
RUN make generate-proto
RUN make build-cli

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN addgroup -g 1001 -S corkscrew && \
    adduser -u 1001 -S corkscrew -G corkscrew

# Set working directory
WORKDIR /app

# Copy CLI binary only (plugins built on-demand by users)
COPY --from=builder /app/build/bin/corkscrew /usr/local/bin/corkscrew

# Create directories for output
RUN mkdir -p /app/output && \
    chown -R corkscrew:corkscrew /app

# Switch to non-root user
USER corkscrew

# Set default plugin directory (users can mount their own plugins)
ENV PLUGIN_DIR=/app/plugins

# Default command
ENTRYPOINT ["corkscrew"]
CMD ["--help"]
