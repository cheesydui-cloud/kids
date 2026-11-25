# Official kids panel image. nft-agent stays on the node host.
# The entrypoint is not the running program, so later upgrades replace a
# side file and restart through the starter.
#
#   docker compose -f docker/panel/compose.yml up -d --build
#
# On a machine that only has Docker, use scripts/install-docker-panel.sh.
# That script downloads the release and does not need the Go toolchain.

FROM golang:1.26-bookworm AS build
WORKDIR /src
ARG VERSION=dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X nft/internal/server.buildVersion=${VERSION}" \
    -o /out/nft-server ./cmd/nft-server

FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      bash ca-certificates curl tzdata passwd util-linux \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 65532 --home-dir /var/lib/nft --no-create-home kids \
 && mkdir -p /usr/local/lib/kids /var/lib/nft \
 && chown -R kids:kids /usr/local/lib/kids /var/lib/nft

COPY --from=build /out/nft-server /usr/local/lib/kids/nft-server
COPY internal/server/panel-start.sh /usr/local/lib/kids/panel-start.sh
COPY internal/server/container-upgrade.sh /usr/local/sbin/nft-upgrade
COPY docker/panel/docker-init.sh /usr/local/sbin/kids-docker-init
RUN sed -i 's/\r$//' \
      /usr/local/lib/kids/panel-start.sh \
      /usr/local/sbin/nft-upgrade \
      /usr/local/sbin/kids-docker-init \
 && cp /usr/local/lib/kids/panel-start.sh /usr/local/sbin/nft-server \
 && chmod 755 \
      /usr/local/lib/kids/nft-server \
      /usr/local/lib/kids/panel-start.sh \
      /usr/local/sbin/nft-server \
      /usr/local/sbin/nft-upgrade \
      /usr/local/sbin/kids-docker-init \
 && chown root:root /usr/local/sbin/kids-docker-init \
 && printf '%s\n' /usr/local/lib/kids/nft-server > /var/lib/nft/nft-server.live \
 && chown kids:kids \
      /usr/local/lib/kids/nft-server \
      /usr/local/lib/kids/panel-start.sh \
      /usr/local/sbin/nft-server \
      /usr/local/sbin/nft-upgrade \
      /var/lib/nft/nft-server.live \
 && chown -R kids:kids /usr/local/lib/kids /var/lib/nft

ENV TZ=Asia/Shanghai \
    NFT_ADDR=:7788 \
    NFT_DB=/var/lib/nft/panel.db

EXPOSE 7788
VOLUME ["/var/lib/nft"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=5 \
  CMD curl -fsS http://127.0.0.1:7788/healthz || exit 1
ENTRYPOINT ["/usr/local/sbin/kids-docker-init"]
