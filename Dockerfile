# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS TARGETARCH
ARG VERSION
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
ENV GOTOOLCHAIN=auto
RUN --mount=type=cache,target=/go/pkg/mod  \
    --mount=type=cache,target=/root/.cache/go-build  \
    go mod download
COPY . .
ENV CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build  \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/thomasd .
FROM gcr.io/distroless/static-debian12:nonroot AS final
WORKDIR /
COPY --from=build /out/thomasd /usr/bin/thomasd
EXPOSE 8081
USER nonroot
ENTRYPOINT ["/usr/bin/thomasd"]