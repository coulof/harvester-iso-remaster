# Stage 1: Compile harvester-cmdline statically using Go 1.26
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Copy go module definitions and vendored dependencies
COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY cmd/ cmd/
COPY internal/ internal/

ARG VERSION=dev

# Compile harvester-cmdline statically offline using vendored dependencies (Air-gap C6)
RUN CGO_ENABLED=0 go build -mod=vendor \
    -ldflags="-s -w -X main.ToolVersion=${VERSION} -X main.PinnedInstallerTag=${VERSION}" \
    -o /harvester-cmdline ./cmd/harvester-cmdline

# Stage 2: Minimal runtime image containing complete ISO remastering toolchain
FROM alpine:3.21

LABEL org.opencontainers.image.source="https://github.com/coulof/harvester-iso-remaster"
LABEL org.opencontainers.image.description="Automated Harvester v1.8 ISO Remastering Toolchain"
LABEL org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache \
    bash \
    xorriso \
    mtools \
    dosfstools \
    gawk \
    coreutils

# Ensure drive geometry check is skipped in mtools and standard sbin paths are in PATH
ENV MTOOLS_SKIP_CHECK=1
ENV PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:${PATH}"

COPY --from=builder /harvester-cmdline /usr/local/bin/harvester-cmdline
COPY remaster-iso.sh /usr/local/bin/remaster-iso.sh
RUN chmod +x /usr/local/bin/remaster-iso.sh /usr/local/bin/harvester-cmdline

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/remaster-iso.sh"]
CMD ["--help"]
