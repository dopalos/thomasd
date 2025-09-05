# syntax=docker/dockerfile:1.7
FROM golang:1.22-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/thomasd ./cmd/thomasd

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="thomasd" \
      org.opencontainers.image.source="https://github.com/dopalos/thomasd"
COPY --from=build /out/thomasd /thomasd
EXPOSE 8081
USER nonroot:nonroot
ENTRYPOINT ["/thomasd"]