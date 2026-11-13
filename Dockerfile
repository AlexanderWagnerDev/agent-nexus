# syntax=docker/dockerfile:1
# Build on the native builder platform and cross-compile for the target image.
# This avoids running the Go compiler under QEMU for every target architecture.
FROM --platform=$BUILDPLATFORM alexanderwagnerdev/alpine:builder AS build
# Go comes from apk; Alpine 3.24 ships 1.26.11, which matches the go directive.
# ca-certificates is needed for the HTTPS module downloads.
RUN apk add --no-cache go ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Target args stay below the module download so that layer is shared by all
# architectures instead of repeated once per platform.
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG TARGETVARIANT
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X github.com/AlexanderWagnerDev/agent-nexus/internal/hub.Version=${VERSION}" \
    -o /out/nexus-hub ./cmd/nexus-hub

FROM alexanderwagnerdev/alpine:latest
ARG VERSION=dev
LABEL org.opencontainers.image.title="AgentNexus" \
      org.opencontainers.image.description="Self-hosted control plane for coding-agent sessions" \
      org.opencontainers.image.source="https://github.com/AlexanderWagnerDev/agent-nexus" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="MIT"
# ca-certificates is required for outbound HTTPS (web push). The nonroot UID
# 65532 matches the previous distroless image (BusyBox adduser rejects UIDs
# above 60000, so the passwd/group entries are written directly), and /data
# keeps its ownership in new named volumes so SQLite can write to it.
RUN apk add --no-cache ca-certificates tzdata \
 && echo 'nexus:x:65532:65532:nexus:/nonexistent:/sbin/nologin' >> /etc/passwd \
 && echo 'nexus:x:65532:' >> /etc/group \
 && mkdir -p /data \
 && chown 65532:65532 /data
COPY --from=build /out/nexus-hub /nexus-hub
USER 65532:65532
VOLUME ["/data"]
EXPOSE 8080
ENV NEXUS_ADDR=0.0.0.0:8080 \
    NEXUS_DATA_DIR=/data
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/nexus-hub", "--healthcheck"]
ENTRYPOINT ["/nexus-hub"]
