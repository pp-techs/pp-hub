# syntax=docker/dockerfile:1
# The web app: Vite build, served by nginx.
#
# Build context is the REPO ROOT, not apps/web — the app depends on
# @workspace/ui, so the whole bun workspace has to be present:
#
#   docker build -f apps/web/Dockerfile .
#
# This container is also the public entry point. The SPA and the API must be
# same-origin: the session cookie is host-only with SameSite=Lax, and the API
# client sends credentials: "same-origin". Split them across hostnames and
# nobody can log in. nginx therefore serves the bundle and proxies /api,
# /auth, /wopi and /s to the server. See docs/deployment.md.

FROM oven/bun:1.4.2-alpine AS build
WORKDIR /src

# Manifests first so the install layer caches independently of source edits.
COPY package.json bun.lock ./
COPY apps/web/package.json apps/web/
COPY packages/ui/package.json packages/ui/
COPY packages/utils/package.json packages/utils/
# --ignore-scripts skips the root "prepare" hook (vp config), which sets up
# git hooks for developers and needs a git binary this image has no reason to
# carry.
RUN --mount=type=cache,target=/root/.bun/install/cache \
    bun install --frozen-lockfile --ignore-scripts

COPY . .
RUN cd apps/web && bun run build

FROM nginx:1.27-alpine AS web
COPY --from=build /src/apps/web/dist /usr/share/nginx/html
COPY apps/web/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
# 127.0.0.1, not localhost: busybox wget tries ::1 first, and a v4-only
# `listen 80` answers nothing there. nginx.conf listens dual-stack now, so this
# is belt and braces — but the check should not depend on that.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1/nginx-health || exit 1
