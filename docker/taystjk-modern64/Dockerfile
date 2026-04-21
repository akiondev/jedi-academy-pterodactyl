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
RUN if printf '%s' "${TAYSTJK_REF}" | grep -Eq '^[0-9a-f]{40}$'; then \
      git clone "${TAYSTJK_REPO}" TaystJK \
      && cd TaystJK \
      && git checkout --detach "${TAYSTJK_REF}"; \
    else \
      git clone --depth 1 --branch "${TAYSTJK_REF}" "${TAYSTJK_REPO}" TaystJK; \
    fi
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

FROM debian:12-slim

ARG TAYSTJK_REPO=https://github.com/taysta/TaystJK.git
ARG TAYSTJK_REF=master
ARG TAYSTJK_COMMIT=unknown

LABEL org.opencontainers.image.title="TaystJK Pterodactyl Runtime"
LABEL org.opencontainers.image.description="Runtime image for TaystJK dedicated server on Pterodactyl built from source"
LABEL org.opencontainers.image.licenses="GPL-2.0-only"
LABEL io.akiondev.taystjk.upstream.repo="${TAYSTJK_REPO}"
LABEL io.akiondev.taystjk.upstream.ref="${TAYSTJK_REF}"
LABEL io.akiondev.taystjk.upstream.commit="${TAYSTJK_COMMIT}"

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

RUN mkdir -p /opt/jka/engine /opt/jka/engine-payload
COPY --from=builder /opt/taystjk/JediAcademy/taystjkded.* /opt/jka/engine/
COPY --from=builder /opt/taystjk/JediAcademy/taystjk /opt/jka/engine-payload/taystjk
COPY --from=antivpn-builder /out/jka-antivpn /usr/local/bin/jka-antivpn
RUN printf '%s\n' "${TAYSTJK_COMMIT}" > /opt/jka/engine/.upstream-commit \
    && printf '%s\n' "${TAYSTJK_REF}" > /opt/jka/engine/.upstream-ref
RUN mkdir -p /opt/jka/docs/addons
RUN mkdir -p /opt/jka/bundled-addons
COPY --chown=container:container docs/addon_readme.md /opt/jka/docs/addons/ADDON_README.md
COPY --chown=container:container docs/addon_readme_advanced.md /opt/jka/docs/addons/ADDON_README_ADVANCED.md
COPY --chown=container:container bundled-addons/ /opt/jka/bundled-addons/
COPY --chown=container:container scripts/common/ /opt/jka/runtime/common/
COPY image/taystjk-modern64/runtime.json /opt/jka/runtime.json
COPY --chown=container:container scripts/entrypoint.sh /entrypoint.sh
RUN ln -s /opt/jka/engine /opt/taystjk-dist \
    && ln -s /opt/jka/runtime /opt/taystjk-runtime \
    && ln -s /opt/jka/docs /opt/taystjk-docs \
    && ln -s /opt/jka/bundled-addons /opt/taystjk-bundled-addons \
    && ln -s /usr/local/bin/jka-antivpn /usr/local/bin/taystjk-antivpn
RUN chmod +x /entrypoint.sh /usr/local/bin/jka-antivpn /opt/jka/engine/taystjkded.*

USER container
ENV USER=container HOME=/home/container
WORKDIR /home/container
ENTRYPOINT ["/bin/bash", "/entrypoint.sh"]
