# syntax=docker/dockerfile:1
#
# Two independent images from one build:
#
#   docker build -f apps/server/Dockerfile --target server .   API server + web app (default)
#   docker build -f apps/server/Dockerfile --target worker .   preview worker
#
# Build context is the REPO ROOT, not apps/server: the server image carries the
# web app, and the app depends on @workspace/ui, so the whole bun workspace has
# to be present. The root .dockerignore is the one that applies.
#
# The compile stages are split per binary on purpose: a `--target server` build
# must not spend time linking the worker, and vice versa. Both share the `deps`
# stage, so the module download happens once. The web build stage is reached
# only by `server`; BuildKit skips it for `worker`.

FROM oven/bun:1.4.2-alpine AS build-web
WORKDIR /src

# Manifests first so the install layer caches independently of source edits.
COPY package.json bun.lock ./
COPY apps/web/package.json apps/web/
COPY packages/ui/package.json packages/ui/
COPY packages/utils/package.json packages/utils/
# The packaged apps' SDK, the first-party apps an administrator uploads (M51)
# and the tool that scaffolds and packs them are workspace members too. The
# image builds none of them, but the lockfile names them.
COPY packages/app-sdk/package.json packages/app-sdk/
COPY examples/apps/kanban/package.json examples/apps/kanban/
COPY examples/apps/checklist/package.json examples/apps/checklist/
COPY tools/pp-apps/package.json tools/pp-apps/
# The product-version carrier is a workspace member too, so the lockfile
# names it; without its manifest `--frozen-lockfile` below fails.
COPY tools/release/package.json tools/release/
# --ignore-scripts skips the root "prepare" hook (vp config), which sets up
# git hooks for developers and needs a git binary this image has no reason to
# carry.
RUN --mount=type=cache,target=/root/.bun/install/cache \
    bun install --frozen-lockfile --ignore-scripts

COPY . .
RUN cd apps/web && bun run build

FROM golang:1.26-alpine AS deps
WORKDIR /src
COPY apps/server/go.mod apps/server/go.sum ./
# go.mod replaces reearth/ygo with the patched copy in third_party/ygo (see its
# PATCHES.md): the replacement must be on disk before the download resolves.
COPY apps/server/third_party/ ./third_party/
# A cache mount instead of a layer: the module cache survives a go.sum change
# and is not baked into the image.
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# -trimpath strips local filesystem paths out of the binary, and -s -w drops
# the symbol and DWARF tables — nothing reads them in production, and they are
# roughly a third of the binary.
FROM deps AS build-server
COPY apps/server/ .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM deps AS build-worker
COPY apps/server/ .
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

# The API server and the web app, one process. Deliberately last, so a bare
# `docker build` still produces the server. Stays free of media tooling; see
# the worker above.
#
# The SPA and the API must be same-origin: the session cookie is host-only
# with SameSite=Lax and the API client sends credentials: "same-origin". The
# server therefore serves the bundle itself (PP_WEB_DIR) next to the API, on
# one listener, rather than a second container fronting it. The bundle reads
# nothing from import.meta.env, so one image is correct for every deployment;
# the one environment-specific value in its security headers
# (PP_ONLYOFFICE_URL, named in the CSP) is read at startup. See
# docs/deployment.md.
#
# alpine already carries ca-certificates-bundle, which the OIDC and S3 clients
# need — do not swap the base for `scratch` without adding it back.
FROM alpine:3.21 AS server
RUN adduser -D app && mkdir -p /data/blobs && chown -R app:app /data
USER app
COPY --from=build-server /out/server /usr/local/bin/server
COPY --from=build-web /src/apps/web/dist /srv/web
ENV PP_WEB_DIR=/srv/web
# 8080 only. The WOPI listener (PP_WOPI_ADDR, :8081) is deliberately not
# EXPOSEd: `docker run -P` publishes every EXPOSEd port, and that one carries
# routes authorized by a token in the URL. Only the compose network reaches it.
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
