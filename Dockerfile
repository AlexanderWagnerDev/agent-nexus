# syntax=docker/dockerfile:1
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/nexus-hub ./cmd/nexus-hub

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/nexus-hub /nexus-hub
VOLUME ["/data"]
EXPOSE 8080
ENV NEXUS_ADDR=0.0.0.0:8080 \
    NEXUS_DATA_DIR=/data
ENTRYPOINT ["/nexus-hub"]
