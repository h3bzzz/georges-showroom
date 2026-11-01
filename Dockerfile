# Storefront image: templ views are regenerated, then the CLI is built as a static binary
# (ONNX Runtime is loaded with purego at run time, so no cgo). Only `serve` runs in production;
# crawling, OCR and classification stay on the workstation and the results are rsync'd over.
FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate -path internal/web/views && \
    CGO_ENABLED=0 go build -tags ORT -trimpath -ldflags="-s -w" -o /out/georges ./cmd/georges

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/*
RUN useradd --system --uid 10001 --create-home georges
COPY --from=build /out/georges /usr/local/bin/georges
USER georges
WORKDIR /home/georges
ENV DATA_DIR=/data
EXPOSE 8080
ENTRYPOINT ["georges"]
CMD ["serve", "--addr", ":8080"]
