# Build the console SPA.
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Build the documentation site. Separate stage so a docs change does
# not invalidate the SPA layer cache, and vice versa.
#
# The theme is vendored under docs/themes/mailyard, so COPY brings it in
# with everything else. It used to be a submodule, and a build context is
# a directory rather than a checkout: if it had never been fetched,
# docs/themes was empty and Hugo built a site of blank pages without
# complaining, so this stage had to test for theme.toml first.
FROM ghcr.io/gohugoio/hugo:v0.164.0 AS docs
USER root
WORKDIR /src/docs
COPY docs/ ./
RUN hugo --minify --cleanDestinationDir

# Build the Go binary. Pure Go (modernc sqlite), so CGO stays off and
# the binary runs on any matching-arch base image.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
COPY --from=docs /src/docs/dist ./docs/dist
ARG VERSION=devel
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X github.com/yousysadmin/mailyard/pkg.Version=${VERSION}" \
    -o /out/mailyard ./cmd/mailyard

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 1000 mailyard
COPY --from=build /out/mailyard /usr/local/bin/mailyard
USER mailyard
WORKDIR /data
# HTTP console/API, SMTP submission, inbound MX.
#
# 587 and 25 are privileged and this image runs as uid 1000. Docker
# sets net.ipv4.ip_unprivileged_port_start=0 in containers, so they
# bind anyway there, but Kubernetes does not - a pod needs
# NET_BIND_SERVICE or the same sysctl, otherwise set MAILYARD_SUBMISSION_ADDR
# and MAILYARD_INBOUND_ADDR to high ports and map them.
EXPOSE 3000 587 25
VOLUME /data
ENTRYPOINT ["mailyard"]
CMD ["serve"]
