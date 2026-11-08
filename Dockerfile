# George's v2 storefront image. Only `serve` runs in production; imports, asset builds and
# model jobs run on the workstation and their output (database + data/raw/assets) is synced.
# The assistant's photo matcher needs cgo (hugot dlopens libonnxruntime.so from DATA_DIR/tools),
# so the binary is a dynamic glibc build on a distroless base with libc.
FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go run github.com/a-h/templ/cmd/templ@v0.3.1100 generate -path internal/web/views && \
    CGO_ENABLED=1 go build -tags ORT -trimpath -ldflags="-s -w" -o /out/georges ./cmd/georges

FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=build /out/georges /georges
ENV DATA_DIR=/data ENV=prod ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/georges"]
CMD ["serve"]
