# kids panel only. The image downloads the official nft-server release.
# nft-agent stays on each node host; it needs the host network and nftables.
#
#   docker compose -f docker/panel/compose.yml up -d --build
#
# Build args:
#   VERSION   release tag, default latest (example: v0.14.0)
#   GH_PROXY  prefix concatenated in front of the GitHub URL, same as install.sh
#             example: https://ghfast.top/

FROM debian:bookworm-slim

ARG VERSION=latest
ARG GH_PROXY=
ARG TARGETARCH

ENV TZ=Asia/Shanghai \
    NFT_ADDR=:7788 \
    NFT_DB=/var/lib/nft/panel.db

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl tzdata \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /var/lib/nft

COPY fetch-panel.sh entrypoint.sh /tmp/kids/
COPY local/ /tmp/kids/local/
RUN sed -i 's/\r$//' /tmp/kids/fetch-panel.sh /tmp/kids/entrypoint.sh \
 && chmod 0755 /tmp/kids/fetch-panel.sh /tmp/kids/entrypoint.sh \
 && VERSION="$VERSION" GH_PROXY="$GH_PROXY" TARGETARCH="$TARGETARCH" \
    sh /tmp/kids/fetch-panel.sh \
 && mv /tmp/kids/entrypoint.sh /usr/local/bin/kids-entrypoint \
 && rm -rf /tmp/kids

EXPOSE 7788
VOLUME ["/var/lib/nft"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=5 \
  CMD curl -fsS http://127.0.0.1:7788/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/kids-entrypoint"]
