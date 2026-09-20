# syntax=docker/dockerfile:1
#
# Two independent images from one Go module:
#
#   docker build --target server .   API server (default)
#   docker build --target worker .   preview worker
#
# The compile stages are split per binary on purpose: a `--target server` build
# must not spend time linking the worker, and vice versa. Both share the `deps`
# stage, so the module download happens once.

FROM golang:1.26-alpine AS deps
WORKDIR /src
COPY go.mod go.sum ./
# A cache mount instead of a layer: the module cache survives a go.sum change
# and is not baked into the image.
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# -trimpath strips local filesystem paths out of the binary, and -s -w drops
# the symbol and DWARF tables — nothing reads them in production, and they are
# roughly a third of the binary.
FROM deps AS build-server
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM deps AS build-worker
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

# The preview worker: build with --target worker.
#
# Optional to deploy. Without it, render:pdf and render:video jobs stay pending
# rather than failing, because ClaimJobs filters on the kinds a process
# registered renderers for. See docs/architecture/preview-pipeline.md.
#
# No EXPOSE and no listener — it reaches Postgres and the blob store, and
# nothing reaches it. That containment is the reason the media tooling lives
# here: pdftoppm and ffmpeg parse hostile bytes in memory-unsafe code, and the
# API process next door holds the session key and the database pool.
FROM alpine:3.21 AS worker
RUN apk add --no-cache poppler-utils ffmpeg \
    && adduser -D app && mkdir -p /data/blobs && chown -R app:app /data
USER app
COPY --from=build-worker /out/worker /usr/local/bin/worker
# No listener to probe, so the worker touches /tmp/worker-healthy on every
# successful dependency check (see healthBeacon in cmd/worker) and this
# checks the file's freshness instead. `find -mmin` over arithmetic on
# `stat -c %Y` because busybox supports it without shell quoting games.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD find /tmp/worker-healthy -mmin -2 | grep -q worker-healthy || exit 1
ENTRYPOINT ["worker"]

# The API server. Deliberately last, so a bare `docker build` still produces
# the server it always did. Stays free of media tooling; see the worker above.
#
# alpine already carries ca-certificates-bundle, which the OIDC and S3 clients
# need — do not swap the base for `scratch` without adding it back.
FROM alpine:3.21 AS server
RUN adduser -D app && mkdir -p /data/blobs && chown -R app:app /data
USER app
COPY --from=build-server /out/server /usr/local/bin/server
EXPOSE 8080
# Mirrors the compose healthcheck so the image is also correct when run by
# something that has no compose file (a bare `docker run`, Kubernetes).
# 127.0.0.1, not localhost: busybox wget resolves ::1 first, and a listener
# that is not dual-stack answers nothing there.
#
# /readyz, not /healthz: a constant-200 liveness probe would restart nothing
# when Postgres or the blob store go unreachable, which is the exact bug this
# healthcheck exists to catch.
HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=10 \
    CMD wget -qO- http://127.0.0.1:8080/readyz || exit 1
ENTRYPOINT ["server"]
