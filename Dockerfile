FROM golang:1.27.2 AS build
# Optional extra CA certificates for networks with TLS inspection: drop *.crt
# files in ./certs (they are gitignored). Harmless when the folder is empty.
COPY certs/ /usr/local/share/ca-certificates/extra/
RUN update-ca-certificates >/dev/null 2>&1 || true
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/teia ./cmd/teia

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/teia /usr/local/bin/teia
ENV TEIA_CONFIG=/etc/teia/teia.yaml
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/teia"]
CMD ["serve"]
