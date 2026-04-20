FROM golang:1.22-bookworm AS antivpn-builder

ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH:-amd64}" \
    go build -trimpath -ldflags="-s -w" -o /out/jka-antivpn ./cmd/taystjk-antivpn

FROM debian:12-slim AS builder

ARG OPENJK_REPO=https://github.com/JACoders/OpenJK.git
ARG OPENJK_REF=master

RUN apt-get update \
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
RUN if printf '%s' "${OPENJK_REF}" | grep -Eq '^[0-9a-f]{40}$'; then \
      git clone "${OPENJK_REPO}" OpenJK \
      && cd OpenJK \
      && git checkout --detach "${OPENJK_REF}"; \
    else \
      git clone --depth 1 --branch "${OPENJK_REF}" "${OPENJK_REPO}" OpenJK; \
    fi
WORKDIR /src/OpenJK

# Build only the modern64 dedicated server (openjkded) and the MP
# server-side game module (jampgame -> base/jampgamex86_64.so). All
# SP, JK2, renderer, UI and client-side cgame targets are disabled
# because this image runs a dedicated server only.
RUN mkdir -p build \
    && cd build \
    && cmake \
        -DCMAKE_BUILD_TYPE=Release \
        -DCMAKE_INSTALL_PREFIX=/opt/openjk \
        -DBuildPortableVersion=ON \
        -DBuildMPEngine=OFF \
        -DBuildMPDed=ON \
        -DBuildMPGame=ON \
        -DBuildMPCGame=OFF \
        -DBuildMPUI=OFF \
        -DBuildMPRdVanilla=OFF \
        -DBuildSPEngine=OFF \
        -DBuildSPGame=OFF \
        -DBuildSPRdVanilla=OFF \
        -DBuildJK2SPEngine=OFF \
        -DBuildJK2SPGame=OFF \
        -DBuildJK2SPRdVanilla=OFF \
        -DBuildDiscordRichPresence=OFF \
        -DBuildTests=OFF \
        .. \
    && make -j"$(nproc)" \
    && make install

FROM debian:12-slim

ARG OPENJK_REPO=https://github.com/JACoders/OpenJK.git
ARG OPENJK_REF=master
ARG OPENJK_COMMIT=unknown

LABEL org.opencontainers.image.title="OpenJK modern64 Pterodactyl Runtime"
LABEL org.opencontainers.image.description="Runtime image for the OpenJK modern64 (linux x86_64) dedicated server on Pterodactyl, built from source"
LABEL org.opencontainers.image.licenses="GPL-2.0-only"
LABEL io.akiondev.openjk.upstream.repo="${OPENJK_REPO}"
LABEL io.akiondev.openjk.upstream.ref="${OPENJK_REF}"
LABEL io.akiondev.openjk.upstream.commit="${OPENJK_COMMIT}"

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        coreutils \
        curl \
        git \
        jq \
        libjpeg62-turbo \
        libpng16-16 \
        libstdc++6 \
        procps \
        python3 \
        python3-pip \
        python3-venv \
        rsync \
        sqlite3 \
        tar \
        unzip \
        wget \
        zlib1g \
    && useradd --create-home --home-dir /home/container --shell /bin/bash container \
    && rm -rf /var/lib/apt/lists/*

# Engine binary lives in /opt/jka/engine; mode-defining payload lives
# in /opt/jka/engine-payload (manifest schema v2). The OpenJK MP
# server-side game module (jampgamex86_64.so) is taken from the
# upstream `base/` install directory, never from any SP or wrong path.
RUN mkdir -p /opt/jka/engine /opt/jka/engine-payload/base
COPY --from=builder /opt/openjk/JediAcademy/openjkded.x86_64 /opt/jka/engine/openjkded.x86_64
COPY --from=builder /opt/openjk/JediAcademy/base/jampgamex86_64.so /opt/jka/engine-payload/base/jampgamex86_64.so
COPY --from=antivpn-builder /out/jka-antivpn /usr/local/bin/jka-antivpn
RUN printf '%s\n' "${OPENJK_COMMIT}" > /opt/jka/engine/.upstream-commit \
    && printf '%s\n' "${OPENJK_REF}" > /opt/jka/engine/.upstream-ref
RUN mkdir -p /opt/jka/docs/addons
RUN mkdir -p /opt/jka/bundled-addons
COPY --chown=container:container docs/addon_readme.md /opt/jka/docs/addons/ADDON_README.md
COPY --chown=container:container docs/addon_readme_advanced.md /opt/jka/docs/addons/ADDON_README_ADVANCED.md
COPY --chown=container:container bundled-addons/ /opt/jka/bundled-addons/
COPY --chown=container:container scripts/common/ /opt/jka/runtime/common/
COPY image/openjk-modern64/runtime.json /opt/jka/runtime.json
COPY --chown=container:container scripts/entrypoint.sh /entrypoint.sh
# Intentionally no /opt/taystjk-* symlinks and no taystjk-antivpn
# alias: those are TaystJK-only migration shims and must not appear
# in the OpenJK image.
RUN chmod +x /entrypoint.sh /usr/local/bin/jka-antivpn /opt/jka/engine/openjkded.x86_64

USER container
ENV USER=container HOME=/home/container
WORKDIR /home/container
ENTRYPOINT ["/bin/bash", "/entrypoint.sh"]
