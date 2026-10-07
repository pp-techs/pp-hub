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
# /auth and /s to the server — but not /wopi, which the Document Server
# reaches over the compose network. See docs/deployment.md.

FROM oven/bun:1.4.2-alpine AS build
WORKDIR /src

# Manifests first so the install layer caches independently of source edits.
COPY package.json bun.lock ./
COPY apps/web/package.json apps/web/
COPY packages/ui/package.json packages/ui/
COPY packages/utils/package.json packages/utils/
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

FROM nginx:1.31-alpine AS web
COPY --from=build /src/apps/web/dist /usr/share/nginx/html

# A template, not a finished config: the Content-Security-Policy has to name
# the Document Server's origin, and hardcoding it would break the first
# deployment that moves the editor. The official image runs envsubst over
# /etc/nginx/templates at startup; NGINX_ENVSUBST_FILTER keeps it to PP_*, so
# nginx's own $host, $remote_addr, $uri and friends are not eaten.
COPY apps/web/nginx.conf /etc/nginx/templates/default.conf.template
ENV NGINX_ENVSUBST_FILTER=PP_

# nginx drops its workers to the unprivileged `nginx` user but keeps the
# master as root, and this container terminates every public request. The
# unprivileged variant of the image is not used because it changes the
# listen port and the whole compose topology assumes 80; instead the master
# runs as nginx too, which works because nothing here binds a port below
# 1024 from inside — the port mapping is the host's business.
RUN touch /var/run/nginx.pid \
    && chown -R nginx:nginx /var/run/nginx.pid /var/cache/nginx /etc/nginx/conf.d
USER nginx

EXPOSE 80
# 127.0.0.1, not localhost: busybox wget tries ::1 first, and a v4-only
# `listen 80` answers nothing there. nginx.conf listens dual-stack now, so this
# is belt and braces — but the check should not depend on that.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1/nginx-health || exit 1
