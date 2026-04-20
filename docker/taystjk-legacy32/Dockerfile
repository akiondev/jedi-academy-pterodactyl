FROM golang:1.22-bookworm AS antivpn-builder

ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

# The anti-VPN supervisor is a regular amd64 Go binary; it runs as a
# separate process alongside the 32-bit TaystJK engine and does not
# need to be built as i386.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH:-amd64}" \
    go build -trimpath -ldflags="-s -w" -o /out/jka-antivpn ./cmd/taystjk-antivpn

FROM debian:12-slim AS builder

ARG TAYSTJK_REPO=https://github.com/taysta/TaystJK.git
ARG TAYSTJK_REF=master

# TaystJK legacy32 is a 32-bit (i386) build of the MP dedicated server
# from the same TaystJK source tree as the default TaystJK image. It
# is published as an EXPERIMENTAL runtime family — the 32-bit target
# is not part of the upstream TaystJK CI matrix for the dedicated
# server and broad mod-binary compatibility has not been verified.
#
# We enable the i386 architecture and install the multilib toolchain
# plus the i386 development libraries TaystJK links against. -m32 in
# CFLAGS/CXXFLAGS/LDFLAGS forces 32-bit (i386) ELF output, mirroring
# the OpenJK legacy32 build.
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
RUN if printf '%s' "${TAYSTJK_REF}" | grep -Eq '^[0-9a-f]{40}$'; then \
      git clone "${TAYSTJK_REPO}" TaystJK \
      && cd TaystJK \
      && git checkout --detach "${TAYSTJK_REF}"; \
    else \
      git clone --depth 1 --branch "${TAYSTJK_REF}" "${TAYSTJK_REPO}" TaystJK; \
    fi
WORKDIR /src/TaystJK

# Build only the legacy32 dedicated server (taystjkded) and the MP
# server-side game module. All client/renderer/UI/SP/JK2 targets are
# disabled because this image runs a dedicated server only. -m32
# forces the multilib toolchain to produce 32-bit (i386) ELF output.
# `setarch i686` makes the kernel report `i686` to `uname -m`, which
# in turn makes CMake set CMAKE_HOST_SYSTEM_PROCESSOR=i686 and the
# upstream CMakeLists.txt then picks the i386 architecture suffix
# (taystjkded.i386 / jampgamei386.so) instead of the host's x86_64.
# The `Assets` custom target is built explicitly because the install
# rule for `taystjk/japro-assets.pk3` is unconditional but the target
# is not built by the default `all` target.
RUN mkdir -p build \
    && cd build \
    && CFLAGS="-m32" CXXFLAGS="-m32" LDFLAGS="-m32" \
       setarch i686 cmake \
        -DCMAKE_BUILD_TYPE=Release \
        -DCMAKE_INSTALL_PREFIX=/opt/taystjk \
        -DBuildPortableVersion=ON \
        -DBuildMPEngine=OFF \
        -DBuildMPDed=ON \
        -DBuildMPGame=ON \
        -DBuildMPCGame=OFF \
        -DBuildMPUI=OFF \
        -DBuildMPRdVanilla=OFF \
        -DBuildMPRdVulkan=OFF \
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
    && setarch i686 make -j"$(nproc)" Assets \
    && setarch i686 make install

FROM debian:12-slim

ARG TAYSTJK_REPO=https://github.com/taysta/TaystJK.git
ARG TAYSTJK_REF=master
ARG TAYSTJK_COMMIT=unknown

LABEL org.opencontainers.image.title="TaystJK legacy32 Pterodactyl Runtime (experimental)"
LABEL org.opencontainers.image.description="EXPERIMENTAL runtime image for the TaystJK legacy32 (linux i386) dedicated server on Pterodactyl, built from source. The 32-bit TaystJK dedicated server target is not part of the upstream CI matrix for the dedicated server and broad mod-binary compatibility has not been verified."
LABEL org.opencontainers.image.licenses="GPL-2.0-only"
LABEL io.akiondev.taystjk-legacy32.upstream.repo="${TAYSTJK_REPO}"
LABEL io.akiondev.taystjk-legacy32.upstream.ref="${TAYSTJK_REF}"
LABEL io.akiondev.taystjk-legacy32.upstream.commit="${TAYSTJK_COMMIT}"
LABEL io.akiondev.taystjk-legacy32.runtime.kind="experimental"

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
# in /opt/jka/engine-payload (manifest schema v2). The TaystJK MP
# server-side payload subdirectory `taystjk/` mirrors the layout used
# by the default 64-bit TaystJK image.
RUN mkdir -p /opt/jka/engine /opt/jka/engine-payload
COPY --from=builder /opt/taystjk/JediAcademy/taystjkded.i386 /opt/jka/engine/taystjkded.i386
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
COPY image/taystjk-legacy32/runtime.json /opt/jka/runtime.json
COPY --chown=container:container scripts/entrypoint.sh /entrypoint.sh
# Intentionally no /opt/taystjk-* migration symlinks and no
# taystjk-antivpn alias: the legacy32 image is a separate experimental
# runtime family and does not inherit the default TaystJK image's
# deprecated migration shims.
RUN chmod +x /entrypoint.sh /usr/local/bin/jka-antivpn /opt/jka/engine/taystjkded.i386

USER container
ENV USER=container HOME=/home/container
WORKDIR /home/container
ENTRYPOINT ["/bin/bash", "/entrypoint.sh"]
