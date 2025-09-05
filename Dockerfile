# syntax=docker/dockerfile:1.7-labs

########## Builder ##########
FROM golang:1.25-alpine AS build
ARG VERSION=dev
ARG BUILD_TAGS=""
ARG APP_MAIN=""

RUN apk add --no-cache git ca-certificates build-base upx
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

# APP_MAIN이 있으면 그 경로로 빌드, 없으면 main 패키지 자동탐지
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod <<'EOF'
set -eux
mkdir -p /out
if [ -n "${APP_MAIN}" ]; then
  APP_PATH="${APP_MAIN}"
else
  APP_PATH="$(go list -f '{{if eq .Name "main"}}{{.Dir}}{{end}}' ./... | awk "NF" | head -n1)"
fi
[ -n "$APP_PATH" ] || { echo "no main package found"; exit 1; }
echo ">> main package: $APP_PATH"

export CGO_ENABLED=0 GOOS="${GOOS:-linux}" GOARCH="${GOARCH:-amd64}"
LDFLAGS="-s -w -X main.version=${VERSION} -buildid="

if [ -n "${BUILD_TAGS}" ]; then
  go build -trimpath -ldflags "$LDFLAGS" -tags "$BUILD_TAGS" -o /out/thomasd "$APP_PATH"
else
  go build -trimpath -ldflags "$LDFLAGS" -o /out/thomasd "$APP_PATH"
fi

upx -q --lzma /out/thomasd || true
EOF

########## Runtime ##########
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/thomasd /usr/local/bin/thomasd
EXPOSE 8081
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/thomasd"]
