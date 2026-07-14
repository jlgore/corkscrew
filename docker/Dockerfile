# Build stage
FROM golang:1.26.2-bookworm AS builder

# Install build dependencies
RUN apt-get update && \
    apt-get install -y --no-install-recommends build-essential git make && \
    rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /app

# Copy source code (needed for local module dependencies)
COPY . .

# Download dependencies (after copying source to resolve local replaces)
RUN go mod download

# Build the CLI from the checked-in generated protobuf sources
RUN make build-cli

# Runtime stage
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd --gid 1001 corkscrew && \
    useradd --uid 1001 --gid corkscrew --create-home corkscrew

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
