---
"@pp-hub/release": minor
---

OnlyOffice Document Server updated from 9.4.0 to 10.0.0.1. 10.0 refuses to call a WOPI host that no `ipfilter` rule names (`allow: *` no longer counts), so without one the editor shows only "Access forbidden". `compose.prod.yml` now writes that rule itself; nothing to do if you use it as shipped. **If you run your own compose (for example a Dokploy copy)** you must (a) bump the image to `onlyoffice/documentserver:10.0.0.1` and (b) give the `onlyoffice` service this entrypoint, whose rule names the host of `PP_WOPI_SERVER_URL` (`server` here; change it if you renamed the service):

```yaml
entrypoint:
  - sh
  - -c
  - |
    cat > /etc/onlyoffice/documentserver/local-production-linux.json <<'EOF'
    {"services":{"CoAuthoring":{"ipfilter":{"rules":[
      {"address":"server","allowed":true},
      {"address":"*","allowed":false}
    ]}}}}
    EOF
    exec /app/ds/run-document-server.sh
```

The Document Server can now only be asked to fetch from the named host. After an unclean disconnect an edit is still saved only 5–10 minutes later, as before.

Developers: `host.docker.internal` is refused by OnlyOffice 10 on OrbStack, so the dev stack gains a `wopi-proxy` service. Re-run `docker compose -f deploy/compose/compose.dev.yml --profile worker up -d`, and set `PP_WOPI_SERVER_URL=http://wopi-proxy:8081` in `apps/server/.env` and `deploy/compose/.env`. If you set `PP_SESSION_KEY` in `apps/server/.env`, set the same value in `deploy/compose/.env`.
