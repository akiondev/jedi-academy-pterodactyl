FROM golang:1.22-bookworm AS antivpn-builder

ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

# The anti-VPN supervisor is a regular amd64 Go binary; it runs as a
# separate process alongside the 32-bit engine and does not need to be
# built as i386.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH:-amd64}" \
    go build -trimpath -ldflags="-s -w" -o /out/jka-antivpn ./cmd/taystjk-antivpn

FROM debian:12-slim AS builder

ARG OPENJK_REPO=https://github.com/JACoders/OpenJK.git
ARG OPENJK_REF=master

# OpenJK legacy32 is a 32-bit (i386) build of the MP dedicated server.
# We enable the i386 architecture and install the multilib toolchain
# plus the i386 development libraries that OpenJK links against. The
# upstream CMakeLists detects the target word size from the compiler
# (CMAKE_SIZEOF_VOID_P==4 -> "i386" suffix on the produced binaries),
# so building with -m32 in CFLAGS/CXXFLAGS/LDFLAGS yields openjkded.i386
# and base/jampgamei386.so under the install prefix.
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
        libjpeg62-turbo-dev:i386 \
        libpng-dev:i386 \
        make \
        zip \
        zlib1g-dev:i386 \
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

# Build only the legacy32 dedicated server (openjkded) and the MP
# server-side game module (jampgame -> base/jampgamei386.so). All SP,
# JK2, renderer (rd-vanilla AND the experimental rd-rend2), UI and
# client-side cgame targets are disabled because this image runs a
# dedicated server only. Disabling rd-rend2 (`BuildMPRend2=OFF`) is
# required: its CMakeLists unconditionally `find_package(OpenGL REQUIRED)`,
# which fails on a headless multilib builder that intentionally does
# not install the i386 OpenGL/GLX development packages.
#
# `-m32` in CFLAGS/CXXFLAGS/LDFLAGS forces the multilib toolchain to
# produce 32-bit (i386) ELF output. `setarch i686` makes the kernel
# report `i686` to `uname -m`, which in turn makes CMake set
# CMAKE_HOST_SYSTEM_PROCESSOR=i686 and the upstream CMakeLists.txt then
# picks the i386 architecture suffix (openjkded.i386 / jampgamei386.so)
# instead of the host's x86_64 — without `setarch i686`, the binaries
# would be named `openjkded.x86_64` even though they are 32-bit code.
RUN mkdir -p build \
    && cd build \
    && CFLAGS="-m32" CXXFLAGS="-m32" LDFLAGS="-m32" \
       setarch i686 cmake \
        -DCMAKE_BUILD_TYPE=Release \
        -DCMAKE_INSTALL_PREFIX=/opt/openjk \
        -DBuildPortableVersion=ON \
        -DBuildMPEngine=OFF \
        -DBuildMPDed=ON \
        -DBuildMPGame=ON \
        -DBuildMPCGame=OFF \
        -DBuildMPUI=OFF \
        -DBuildMPRdVanilla=OFF \
        -DBuildMPRend2=OFF \
        -DBuildSPEngine=OFF \
        -DBuildSPGame=OFF \
        -DBuildSPRdVanilla=OFF \
        -DBuildJK2SPEngine=OFF \
        -DBuildJK2SPGame=OFF \
        -DBuildJK2SPRdVanilla=OFF \
        -DBuildDiscordRichPresence=OFF \
        -DBuildTests=OFF \
        .. \
    && setarch i686 make -j"$(nproc)" \
    && setarch i686 make install

FROM debian:12-slim

ARG OPENJK_REPO=https://github.com/JACoders/OpenJK.git
ARG OPENJK_REF=master
ARG OPENJK_COMMIT=unknown

LABEL org.opencontainers.image.title="OpenJK legacy32 Pterodactyl Runtime"
LABEL org.opencontainers.image.description="Runtime image for the OpenJK legacy32 (linux i386) dedicated server on Pterodactyl, built from source"
LABEL org.opencontainers.image.licenses="GPL-2.0-only"
LABEL io.akiondev.openjk.upstream.repo="${OPENJK_REPO}"
LABEL io.akiondev.openjk.upstream.ref="${OPENJK_REF}"
LABEL io.akiondev.openjk.upstream.commit="${OPENJK_COMMIT}"

# Runtime needs the i386 versions of every shared library the engine
# links against, plus the i386 dynamic loader (pulled in by libc6:i386).
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

# Engine binary lives in /opt/jka/engine; mode-defining payload lives
# in /opt/jka/engine-payload (manifest schema v2). The OpenJK MP
# server-side game module (jampgamei386.so) is taken from the upstream
# `base/` install directory, never from any SP or wrong path.
RUN mkdir -p /opt/jka/engine /opt/jka/engine-payload/base
COPY --from=builder /opt/openjk/JediAcademy/openjkded.i386 /opt/jka/engine/openjkded.i386
COPY --from=builder /opt/openjk/JediAcademy/base/jampgamei386.so /opt/jka/engine-payload/base/jampgamei386.so
COPY --from=antivpn-builder /out/jka-antivpn /usr/local/bin/jka-antivpn
RUN printf '%s\n' "${OPENJK_COMMIT}" > /opt/jka/engine/.upstream-commit \
    && printf '%s\n' "${OPENJK_REF}" > /opt/jka/engine/.upstream-ref
RUN mkdir -p /opt/jka/docs/addons
RUN mkdir -p /opt/jka/bundled-addons
COPY --chown=container:container docs/addon_readme.md /opt/jka/docs/addons/ADDON_README.md
COPY --chown=container:container docs/addon_readme_advanced.md /opt/jka/docs/addons/ADDON_README_ADVANCED.md
COPY --chown=container:container bundled-addons/ /opt/jka/bundled-addons/
COPY --chown=container:container scripts/common/ /opt/jka/runtime/common/
COPY image/openjk-legacy32/runtime.json /opt/jka/runtime.json
COPY --chown=container:container scripts/entrypoint.sh /entrypoint.sh
# Intentionally no /opt/taystjk-* symlinks and no taystjk-antivpn
# alias: those are TaystJK-only migration shims and must not appear
# in the OpenJK image.
RUN chmod +x /entrypoint.sh /usr/local/bin/jka-antivpn /opt/jka/engine/openjkded.i386

USER container
ENV USER=container HOME=/home/container
WORKDIR /home/container
ENTRYPOINT ["/bin/bash", "/entrypoint.sh"]
