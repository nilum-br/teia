# Cross-compiles on the build machine's native platform, so multi-arch images
# (linux/amd64, linux/arm64) build fast: docker buildx build --platform ...
FROM --platform=$BUILDPLATFORM golang:1.27.3 AS build
# Optional extra CA certificates for networks with TLS inspection: drop *.crt
# files in ./certs (they are gitignored). Harmless when the folder is empty.
COPY certs/ /usr/local/share/ca-certificates/extra/
RUN update-ca-certificates >/dev/null 2>&1 || true
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH
RUN mkdir -p /out/history
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/teia ./cmd/teia

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/teia /usr/local/bin/teia
COPY --from=build --chown=65532:65532 /out/history /var/lib/teia/history
ENV TEIA_CONFIG=/etc/teia/teia.yaml
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/teia"]
CMD ["serve"]
