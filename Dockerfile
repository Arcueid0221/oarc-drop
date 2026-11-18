# Stage 1: Build Vue 3 Liquid Glass SPA
FROM node:22-bookworm-slim AS web-builder
WORKDIR /web
COPY web/package.json ./
RUN npm install --no-audit --no-fund
COPY web/ ./
RUN npm run check

# Stage 2: Build static Go server
FROM golang:1.24-bookworm AS go-builder
WORKDIR /src
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/oarc-drop ./cmd/oarc-drop

# Stage 3: Minimal runtime with rclone + CA certs
FROM debian:bookworm-slim
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl rclone tzdata \
 && rm -rf /var/lib/apt/lists/* \
 && useradd -r -u 10001 -m -s /usr/sbin/nologin oarcdrop \
 && mkdir -p /app/web/dist /data \
 && chown -R oarcdrop:oarcdrop /app /data

COPY --from=go-builder /out/oarc-drop /usr/local/bin/oarc-drop
COPY --from=web-builder /web/dist /app/web/dist

USER oarcdrop
WORKDIR /app
ENV LISTEN_ADDR=":8095" \
    DATA_DIR="/data" \
    WEB_DIR="/app/web/dist"
EXPOSE 8095
ENTRYPOINT ["/usr/local/bin/oarc-drop"]
