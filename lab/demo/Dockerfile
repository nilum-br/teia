# Built from the repository root: docker build -f lab/demo/Dockerfile .
FROM golang:1.27.3 AS build
COPY certs/ /usr/local/share/ca-certificates/extra/
RUN update-ca-certificates >/dev/null 2>&1 || true
WORKDIR /src
COPY lab/demo/go.mod lab/demo/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY lab/demo/ .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/demo .

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/demo /usr/local/bin/demo
ENTRYPOINT ["/usr/local/bin/demo"]
