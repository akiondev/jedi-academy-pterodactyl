FROM golang:1.22-bookworm AS antivpn-builder

ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

# The anti-VPN supervisor is a regular amd64 Go binary; it runs as a
# separate process alongside the 32-bit user-supplied engine and does
# not need to be built as i386.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH:-amd64}" \
    go build -trimpath -ldflags="-s -w" -o /out/jka-antivpn ./cmd/taystjk-antivpn

FROM debian:12-slim AS builder

ARG YBEPROXY_REPO=https://github.com/Yberion/JKA_YBEProxy.git
ARG YBEPROXY_REF=main

# YBEProxy is a 32-bit (i386) game-module proxy library that replaces
# `base/jampgamei386.so`. It is NOT a dedicated server engine; the
# engine itself must come from another runtime (vanilla, OpenJK
# legacy32, …) and is operator-supplied at runtime. Upstream's
# CMakeLists targets a 32-bit build via `-DTARGET_ARCH=x86` plus the
# multilib toolchain.
RUN dpkg --add-architecture i386 \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        cmake \
        curl \
        g++ \
        g++-multilib \
        gcc \
        gcc-multilib \
        git \
        make \
        zip \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN if printf '%s' "${YBEPROXY_REF}" | grep -Eq '^[0-9a-f]{40}$'; then \
      git clone "${YBEPROXY_REPO}" JKA_YBEProxy \
      && cd JKA_YBEProxy \
      && git checkout --detach "${YBEPROXY_REF}"; \
    else \
      git clone --depth 1 --branch "${YBEPROXY_REF}" "${YBEPROXY_REPO}" JKA_YBEProxy; \
    fi
WORKDIR /src/JKA_YBEProxy

# Build the YBEProxy game module as a 32-bit (i386) shared library.
# Upstream's CMake selects `i386` as the architecture suffix on Linux
# when -DTARGET_ARCH=x86 is set on a 64-bit host, producing
# `jampgamei386.so`. The build directory holds the produced artifact
# (CMAKE_RUNTIME_OUTPUT_DIRECTORY = CMAKE_CURRENT_BINARY_DIR).
RUN mkdir -p build \
    && cd build \
    && cmake \
        -DCMAKE_BUILD_TYPE=Release \
        -DTARGET_ARCH=x86 \
        -DBuildJKA_YBEProxy=ON \
        .. \
    && make -j"$(nproc)" \
    && mkdir -p /out/base \
    && find . -type f -name 'jampgamei386.so' -exec cp -v {} /out/base/jampgamei386.so \;

FROM debian:12-slim

ARG YBEPROXY_REPO=https://github.com/Yberion/JKA_YBEProxy.git
ARG YBEPROXY_REF=main
ARG YBEPROXY_COMMIT=unknown

LABEL org.opencontainers.image.title="YBEProxy legacy32 Pterodactyl Runtime"
LABEL org.opencontainers.image.description="Runtime image providing a 32-bit (i386) Linux environment for an operator-supplied Jedi Academy MP dedicated server engine, with the YBEProxy game module (jampgamei386.so) built from source and shipped as the image-managed mode payload. The image does not build, ship or auto-update any server engine; it ships only the YBEProxy game-module proxy."
LABEL org.opencontainers.image.licenses="GPL-2.0-only"
LABEL io.akiondev.ybeproxy.runtime.kind="manual-engine"
LABEL io.akiondev.ybeproxy.runtime.arch="i386"
LABEL io.akiondev.ybeproxy.upstream.repo="${YBEPROXY_REPO}"
LABEL io.akiondev.ybeproxy.upstream.ref="${YBEPROXY_REF}"
LABEL io.akiondev.ybeproxy.upstream.commit="${YBEPROXY_COMMIT}"

# Runtime needs the i386 versions of every shared library the
# operator-supplied 32-bit engine and the YBEProxy module typically
# link against, plus the i386 dynamic loader (pulled in by libc6:i386).
RUN dpkg --add-architecture i386 \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        coreutils \
        curl \
        git \
        jq \
        libc6:i386 \
        libjpeg62-turbo:i386 \
        libpng16-16:i386 \
        libstdc++6:i386 \
        procps \
        python3 \
        python3-pip \
        python3-venv \
        rsync \
        sqlite3 \
        tar \
        unzip \
        wget \
        zlib1g:i386 \
    && useradd --create-home --home-dir /home/container --shell /bin/bash container \
    && rm -rf /var/lib/apt/lists/*

# /opt/jka/engine intentionally stays empty: the YBEProxy image does
# not ship a server engine. The mode-defining payload is the YBEProxy
# game-module proxy `jampgamei386.so`, staged into the live game tree
# on startup using the same sha256 manifest mechanism as the OpenJK and
# TaystJK images.
RUN mkdir -p /opt/jka/engine /opt/jka/engine-payload/base
COPY --from=builder /out/base/jampgamei386.so /opt/jka/engine-payload/base/jampgamei386.so
COPY --from=antivpn-builder /out/jka-antivpn /usr/local/bin/jka-antivpn
RUN printf '%s\n' "${YBEPROXY_COMMIT}" > /opt/jka/engine/.upstream-commit \
    && printf '%s\n' "${YBEPROXY_REF}" > /opt/jka/engine/.upstream-ref
RUN mkdir -p /opt/jka/docs/addons
RUN mkdir -p /opt/jka/bundled-addons
COPY --chown=container:container docs/addon_readme.md /opt/jka/docs/addons/ADDON_README.md
COPY --chown=container:container docs/addon_readme_advanced.md /opt/jka/docs/addons/ADDON_README_ADVANCED.md
COPY --chown=container:container bundled-addons/ /opt/jka/bundled-addons/
COPY --chown=container:container scripts/common/ /opt/jka/runtime/common/
COPY image/ybeproxy-legacy32/runtime.json /opt/jka/runtime.json
COPY --chown=container:container scripts/entrypoint.sh /entrypoint.sh
# Intentionally no /opt/taystjk-* symlinks and no taystjk-antivpn
# alias: those are TaystJK-only migration shims and must not appear
# in the YBEProxy image.
RUN chmod +x /entrypoint.sh /usr/local/bin/jka-antivpn

USER container
ENV USER=container HOME=/home/container
WORKDIR /home/container
ENTRYPOINT ["/bin/bash", "/entrypoint.sh"]
