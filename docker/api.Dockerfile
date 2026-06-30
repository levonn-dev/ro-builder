# Multi-stage build for the ro-builder API binary. The catalog and server
# profile YAMLs are embedded into the binary via //go:embed, so the
# runtime image needs only the binary itself and connects to PostgreSQL
# over the network (no local writable state).

# --- builder ---
FROM golang:1.26-alpine AS builder

# git is needed for `go mod download` on modules with VCS deps.
RUN apk add --no-cache git

WORKDIR /src

# Cache deps in a separate layer; re-runs of the build skip download
# unless go.mod / go.sum changed.
COPY go.mod go.sum ./
RUN go mod download

# Copy the source tree. .dockerignore prunes the things we don't need
# (tmp/, ref_docs/, etc.) so the context stays small.
COPY . .

# Build statically (CGO off) so the runtime image can be a tiny base.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/api ./cmd/api

# --- runtime ---
FROM alpine:3.20

# ca-certificates lets the binary make HTTPS calls (Anthropic API);
# tzdata covers any time-zone formatting in logs.
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S -g 1000 app && adduser -S -u 1000 -G app app

USER app
WORKDIR /home/app

COPY --from=builder /out/api /usr/local/bin/api

# Defaults match docker-compose.yml. ADDR and SIDECAR_URL can be
# overridden per-deploy without rebuilding.
ENV ADDR=":8080" \
    SIDECAR_URL="http://sidecar:7401"

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/api"]
