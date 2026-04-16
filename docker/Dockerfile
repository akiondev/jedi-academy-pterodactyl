FROM debian:12-slim AS builder

ARG TAYSTJK_REPO=https://github.com/taysta/TaystJK.git
ARG TAYSTJK_REF=master

RUN dpkg --add-architecture i386 \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        cmake \
        curl \
        g++ \
        gcc \
        git \
        libjpeg-dev \
        libpng-dev \
        make \
        zip \
        zlib1g-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN git clone --depth 1 --branch "${TAYSTJK_REF}" "${TAYSTJK_REPO}" TaystJK
WORKDIR /src/TaystJK

RUN mkdir -p build \
    && cd build \
    && cmake \
        -DCMAKE_BUILD_TYPE=Release \
        -DCMAKE_INSTALL_PREFIX=/opt/taystjk \
        -DBuildPortableVersion=ON \
        -DBuildMPEngine=OFF \
        -DBuildMPRdVanilla=OFF \
        -DBuildMPRdVulkan=OFF \
        -DBuildMPRend2=OFF \
        -DBuildMPUI=OFF \
        -DBuildMPGame=ON \
        -DBuildMPCGame=ON \
        -DBuildDiscordRichPresence=OFF \
        -DBuildTests=OFF \
        .. \
    && make -j"$(nproc)" \
    && make install

FROM ghcr.io/pterodactyl/yolks:debian

LABEL org.opencontainers.image.title="TaystJK Pterodactyl Runtime"
LABEL org.opencontainers.image.description="Runtime image for TaystJK dedicated server on Pterodactyl built from source"
LABEL org.opencontainers.image.licenses="GPL-2.0-only"

USER root
RUN set -eux; \
    for list in /etc/apt/sources.list /etc/apt/sources.list.d/*.list; do \
      [ -f "$list" ] || continue; \
      sed -i 's|deb.debian.org/debian|archive.debian.org/debian|g' "$list"; \
      sed -i 's|security.debian.org/debian-security|archive.debian.org/debian-security|g' "$list"; \
    done; \
    printf 'Acquire::Check-Valid-Until "false";\nAcquire::AllowInsecureRepositories "true";\n' > /etc/apt/apt.conf.d/99archive-fallback; \
    apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        coreutils \
        curl \
        libjpeg62-turbo \
        libpng16-16 \
        libstdc++6 \
        tar \
        unzip \
        zlib1g \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /opt/taystjk-dist
COPY --from=builder /opt/taystjk/JediAcademy/taystjkded.* /opt/taystjk-dist/
COPY --from=builder /opt/taystjk/JediAcademy/taystjk /opt/taystjk-dist/taystjk
COPY --chown=container:container scripts/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh /opt/taystjk-dist/taystjkded.*

USER container
WORKDIR /home/container
ENTRYPOINT ["/bin/bash", "/entrypoint.sh"]
