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
# Alpine (not distroless) so the PostgreSQL client tools are available: the
# Backup & Restore feature shells out to pg_dump / pg_restore / psql.
FROM alpine:3.20

RUN apk add --no-cache postgresql16-client ca-certificates \
 && addgroup -S app && adduser -S -G app app

COPY --from=build /out/zahirdbman /usr/local/bin/zahirdbman

EXPOSE 8080
USER app

ENTRYPOINT ["/usr/local/bin/zahirdbman"]
