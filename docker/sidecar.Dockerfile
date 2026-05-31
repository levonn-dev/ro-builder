# Calc sidecar image: Node 24 with jsdom and the calc backend's vendor
# files baked in.
#
# Backends are pluggable (see calc-sidecar/src/backends/registry.ts);
# the rocalc backend wants vendor files under calc-sidecar/vendor/rocalc/,
# the stub backend wants nothing. The vendor/ dir itself is tracked
# (via .gitkeep), so `COPY vendor` below always succeeds; an empty
# vendor is valid for backends that don't need on-disk assets.
#
# BYO calc: users drop their own copy of the chosen backend's files
# (e.g. rocalc) into calc-sidecar/vendor/<engine>/.

FROM node:24-alpine

# tini gives us proper signal handling so `docker stop` cleanly exits
# the Node process instead of waiting for the 10s default timeout.
RUN apk add --no-cache tini && \
    deluser node 2>/dev/null || true && \
    delgroup node 2>/dev/null || true && \
    addgroup -S -g 1000 app && adduser -S -u 1000 -G app app

WORKDIR /app

# Cache layer for npm install; skipped on subsequent builds unless
# package*.json changed.
COPY calc-sidecar/package.json calc-sidecar/package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund

# Source tree + vendored rocalc. The .dockerignore at the repo root
# excludes test/ and tsconfig outputs to keep the image lean.
COPY calc-sidecar/server.ts ./server.ts
COPY calc-sidecar/src ./src
COPY calc-sidecar/vendor ./vendor

USER app

ENV PORT=7401
EXPOSE 7401

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["node", "server.ts"]
