# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src
COPY go.mod main.go cache.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/wsm-torznab .

FROM alpine:3.23 AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=1000:1000 /out/wsm-torznab /wsm-torznab
USER 1000:1000
EXPOSE 8787
VOLUME /cache
HEALTHCHECK --interval=30s --timeout=8s --start-period=20s --retries=3 CMD ["/wsm-torznab","--health"]
LABEL org.opencontainers.image.source="https://github.com/GodsQuantum/Cloud9-Watchsomuch-Torznab" org.opencontainers.image.title="Cloud9-Watchsomuch-Torznab" org.opencontainers.image.description="Lightweight read-only searchable Torznab catalog adapter with Go standard library"
ENTRYPOINT ["/wsm-torznab"]
