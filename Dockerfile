# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.23-alpine AS build

WORKDIR /src

# Cache module downloads separately from the source for faster rebuilds.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the rest of the source and build a static binary. The web assets are
# embedded via go:embed, so the resulting binary is fully self-contained.
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/zahirdbman ./cmd/server

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

# TLS roots come with the distroless static image, enabling sslmode=require.
COPY --from=build /out/zahirdbman /usr/local/bin/zahirdbman

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/zahirdbman"]
