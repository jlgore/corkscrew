# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make protobuf protobuf-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Install protobuf Go plugins
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate protobuf code and build
RUN make generate-proto
RUN make build-cli
RUN make build-example-plugins

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN addgroup -g 1001 -S corkscrew && \
    adduser -u 1001 -S corkscrew -G corkscrew

# Set working directory
WORKDIR /app

# Copy binaries from builder
COPY --from=builder /app/cmd/corkscrew/corkscrew /usr/local/bin/corkscrew
COPY --from=builder /app/plugins/ /app/plugins/

# Create directories for output
RUN mkdir -p /app/output && \
    chown -R corkscrew:corkscrew /app

# Switch to non-root user
USER corkscrew

# Set default plugin directory
ENV PLUGIN_DIR=/app/plugins

# Default command
ENTRYPOINT ["corkscrew"]
CMD ["--help"]
