# syntax=docker/dockerfile:1
# Build on the native builder platform and cross-compile for the target image.
# This avoids running the Go compiler under QEMU for linux/arm64.
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X github.com/AlexanderWagnerDev/agent-nexus/internal/hub.Version=${VERSION}" \
    -o /out/nexus-hub ./cmd/nexus-hub

# Prepare the data directory with the distroless nonroot UID (65532). Docker
# copies this ownership into a new named volume, so SQLite can write to /data.
FROM build AS data
RUN mkdir -p /data && chown 65532:65532 /data

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="AgentNexus" \
      org.opencontainers.image.description="Self-hosted control plane for coding-agent sessions" \
      org.opencontainers.image.source="https://github.com/AlexanderWagnerDev/agent-nexus" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/nexus-hub /nexus-hub
COPY --from=data --chown=65532:65532 /data /data
VOLUME ["/data"]
EXPOSE 8080
ENV NEXUS_ADDR=0.0.0.0:8080 \
    NEXUS_DATA_DIR=/data
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/nexus-hub", "--healthcheck"]
ENTRYPOINT ["/nexus-hub"]
