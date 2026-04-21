FROM golang:1.22-bookworm AS antivpn-builder

ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

# The anti-VPN supervisor is a regular amd64 Go binary; it runs as a
# separate process alongside the 32-bit user-supplied vanilla engine
# and does not need to be built as i386.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH:-amd64}" \
    go build -trimpath -ldflags="-s -w" -o /out/jka-antivpn ./cmd/taystjk-antivpn

FROM debian:12-slim

LABEL org.opencontainers.image.title="Vanilla legacy32 Pterodactyl Runtime"
LABEL org.opencontainers.image.description="Runtime image providing a 32-bit (i386) Linux environment for an operator-supplied vanilla Jedi Academy MP dedicated server binary on Pterodactyl. The image does not build, ship, fetch or auto-update the vanilla engine; the operator must upload their own legally obtained 32-bit dedicated binary."
LABEL org.opencontainers.image.licenses="GPL-2.0-only"
LABEL io.akiondev.vanilla.runtime.kind="manual"
LABEL io.akiondev.vanilla.runtime.arch="i386"

# Vanilla legacy32 is a manual runtime family: the image deliberately
# does NOT build or ship a vanilla Jedi Academy dedicated server
# binary. Operators must upload their own legally owned 32-bit Linux
# dedicated server binary (commonly named `linuxjampded`, `jampDed.i386`
# or similar) into their server volume.
#
# The image's only job is to provide a clean 32-bit (i386) capable
# Debian environment with the i386 dynamic loader and the i386 versions
# of every shared library the stock vanilla engine typically links
# against, plus the same shared operational tooling (anti-VPN
# supervisor, addon loader, runtime helpers, addon docs/examples) used
# by the other runtime images.
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

# /opt/jka/engine intentionally stays empty: there is no image-managed
# engine binary for the vanilla family. /opt/jka/engine-payload also
# stays empty for the same reason — there is no image-managed mode
# payload (no jampgamei386.so is shipped). The runtime sync code
# tolerates an empty engine_dist_dir and simply logs that no
# image-provided binaries were found.
RUN mkdir -p /opt/jka/engine /opt/jka/engine-payload
COPY --from=antivpn-builder /out/jka-antivpn /usr/local/bin/jka-antivpn
RUN printf '%s\n' "manual" > /opt/jka/engine/.upstream-commit \
    && printf '%s\n' "manual" > /opt/jka/engine/.upstream-ref
RUN mkdir -p /opt/jka/docs/addons
RUN mkdir -p /opt/jka/bundled-addons
COPY --chown=container:container docs/addon_readme.md /opt/jka/docs/addons/ADDON_README.md
COPY --chown=container:container docs/addon_readme_advanced.md /opt/jka/docs/addons/ADDON_README_ADVANCED.md
COPY --chown=container:container bundled-addons/ /opt/jka/bundled-addons/
COPY --chown=container:container scripts/common/ /opt/jka/runtime/common/
COPY image/vanilla-legacy32/runtime.json /opt/jka/runtime.json
COPY --chown=container:container scripts/entrypoint.sh /entrypoint.sh
# Intentionally no /opt/taystjk-* symlinks and no taystjk-antivpn
# alias: those are TaystJK-only migration shims and must not appear
# in the vanilla image.
RUN chmod +x /entrypoint.sh /usr/local/bin/jka-antivpn

USER container
ENV USER=container HOME=/home/container
WORKDIR /home/container
ENTRYPOINT ["/bin/bash", "/entrypoint.sh"]
