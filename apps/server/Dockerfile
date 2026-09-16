FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server
RUN CGO_ENABLED=0 go build -o /out/worker ./cmd/worker

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
COPY --from=build /out/worker /usr/local/bin/worker
ENTRYPOINT ["worker"]

# The API server. Deliberately last, so a bare `docker build` still produces
# the server it always did. Stays free of media tooling; see the worker above.
FROM alpine:3.21 AS server
RUN adduser -D app && mkdir -p /data/blobs && chown -R app:app /data
USER app
COPY --from=build /out/server /usr/local/bin/server
EXPOSE 8080
ENTRYPOINT ["server"]
