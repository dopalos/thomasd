# syntax=docker/dockerfile:1

########################
# Builder (multi-arch) #
########################
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build
WORKDIR /src

# (안정성) Go 모듈 프록시 지정
ENV GOPROXY=https://proxy.golang.org,direct

# go.mod/go.sum만 먼저 복사 → 모듈 캐시 레이어
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# 나머지 소스 복사
COPY . .

# 빌드 타깃(멀티아치) + 버전 주입
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

# 정적 바이너리 빌드
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/thomasd ./cmd/thomasd

########################
#    Runtime (tiny)    #
########################
FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=build /out/thomasd /usr/local/bin/thomasd

EXPOSE 8081
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/thomasd"]
