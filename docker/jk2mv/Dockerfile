FROM debian:12-slim AS jk2mv-fetch

ARG JK2MV_VERSION=1.4.1
ARG JK2MV_DOWNLOAD_URL=https://github.com/mvdevs/jk2mv/releases/download/1.4.1/jk2mv-v1.4.1-dedicated.zip

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        unzip \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /tmp/jk2mv
RUN curl -fsSL -o jk2mv-dedicated.zip "${JK2MV_DOWNLOAD_URL}" \
    && unzip -q jk2mv-dedicated.zip -d /tmp/jk2mv/extracted \
    && ls -R /tmp/jk2mv/extracted >/tmp/jk2mv/contents.txt \
    && mkdir -p /opt/jk2mv/base \
    && cp /tmp/jk2mv/extracted/linux-amd64/jk2mvded /opt/jk2mv/jk2mvded \
    && cp /tmp/jk2mv/extracted/base/assetsmv.pk3 /opt/jk2mv/base/assetsmv.pk3 \
    && cp /tmp/jk2mv/extracted/base/assetsmv2.pk3 /opt/jk2mv/base/assetsmv2.pk3 \
    && chmod +x /opt/jk2mv/jk2mvded \
    && rm -rf /tmp/jk2mv/extracted /tmp/jk2mv/jk2mv-dedicated.zip

FROM debian:12-slim

ARG JK2MV_VERSION=1.4.1
ARG JK2MV_DOWNLOAD_URL=https://github.com/mvdevs/jk2mv/releases/download/1.4.1/jk2mv-v1.4.1-dedicated.zip

LABEL org.opencontainers.image.title="JK2MV Pterodactyl Runtime"
LABEL org.opencontainers.image.description="Runtime image for the Jedi Outcast JK2MV dedicated server on Pterodactyl"
LABEL org.opencontainers.image.licenses="GPL-2.0-only"
LABEL io.akiondev.jk2mv.upstream.repo="https://github.com/mvdevs/jk2mv"
LABEL io.akiondev.jk2mv.upstream.version="${JK2MV_VERSION}"
LABEL io.akiondev.jk2mv.upstream.download_url="${JK2MV_DOWNLOAD_URL}"

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        coreutils \
        curl \
        procps \
        unzip \
    && useradd --create-home --home-dir /home/container --shell /bin/bash container \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /opt/jk2mv/base
COPY --from=jk2mv-fetch /opt/jk2mv/jk2mvded /opt/jk2mv/jk2mvded
COPY --from=jk2mv-fetch /opt/jk2mv/base/assetsmv.pk3 /opt/jk2mv/base/assetsmv.pk3
COPY --from=jk2mv-fetch /opt/jk2mv/base/assetsmv2.pk3 /opt/jk2mv/base/assetsmv2.pk3
RUN chmod +x /opt/jk2mv/jk2mvded \
    && printf '%s\n' "${JK2MV_VERSION}" > /opt/jk2mv/.upstream-version

COPY image/jk2mv/runtime.json /opt/jk2mv/runtime.json
COPY scripts/jk2mv/entrypoint.sh /entrypoint.sh
COPY scripts/jk2mv/start_jk2mv.sh /usr/local/bin/start_jk2mv.sh
RUN chmod +x /entrypoint.sh /usr/local/bin/start_jk2mv.sh \
    && chown -R container:container /opt/jk2mv

USER container
ENV USER=container HOME=/home/container
WORKDIR /home/container

ENTRYPOINT ["/bin/bash", "/entrypoint.sh"]
CMD ["/usr/local/bin/start_jk2mv.sh"]
