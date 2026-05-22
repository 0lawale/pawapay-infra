# =============================================================================
# Multi-stage Dockerfile for the ConfigMirror operator
#
# SECURITY best practices applied:
#   Stage 1 (builder): Full Go toolchain — never shipped to production
#   Stage 2 (runtime): Distroless base — no shell, no package manager,
#                      minimal attack surface, no root user
#
# The final image contains ONLY the compiled binary and TLS certificates.
# An attacker who escapes the container has nothing useful to work with.
# =============================================================================

# -----------------------------------------------------------------------------
# Stage 1: Builder
# Uses the official Go image to compile the operator binary.
# CGO_ENABLED=0 produces a fully static binary with no libc dependency.
# -----------------------------------------------------------------------------
FROM golang:1.22-alpine AS builder

# Install git for go module downloads (needed for private modules via HTTPS)
RUN apk add --no-cache git ca-certificates

WORKDIR /workspace

# Copy go module files first — Docker layer caches the dependency download
# so it only re-runs when go.mod or go.sum changes, not on every code change.
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY api/      api/
COPY internal/ internal/
COPY cmd/      cmd/

# Build the operator binary
# CGO_ENABLED=0  — static binary, no C libraries required at runtime
# GOOS=linux     — target the container OS explicitly
# -trimpath      — remove local filesystem paths from the binary (security)
# -ldflags       — strip debug info to reduce binary size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w -extldflags '-static'" \
    -o /workspace/manager \
    ./cmd/main.go

# -----------------------------------------------------------------------------
# Stage 2: Runtime — distroless
#
# gcr.io/distroless/static-debian12 contains:
#   - CA certificates (for TLS to AWS APIs and RDS)
#   - timezone data
#   - glibc (needed for static Go binaries)
#   - NO shell, NO curl, NO package manager, NO extra tools
#
# This dramatically reduces the attack surface compared to alpine or ubuntu.
# -----------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /

# Copy only the compiled binary from the builder stage
COPY --from=builder /workspace/manager .

# SECURITY: Run as non-root user (uid 65532 = "nonroot" in distroless)
# The :nonroot tag sets USER nonroot by default, but we make it explicit.
USER 65532:65532

# Expose metrics and health probe ports (documentation only — not a firewall)
EXPOSE 8080 8081

# The binary is the entrypoint — no shell wrapping
ENTRYPOINT ["/manager"]
