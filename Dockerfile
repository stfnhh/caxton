# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /src

RUN apk add --no-cache \
    binutils \
    ca-certificates \
    upx

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    GOARM=${TARGETVARIANT#v} \
    go build \
    -buildvcs=false \
    -trimpath \
    -tags="netgo osusergo" \
    -ldflags="-s -w -buildid=" \
    -o /caxton \
    ./cmd/caxton \
    && if [ "${TARGETARCH:-amd64}" = "amd64" ]; then \
      strip /caxton || true; \
      upx --best --lzma /caxton || echo "UPX compression skipped"; \
    fi


FROM scratch

WORKDIR /app

COPY --from=build /caxton /app/caxton
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENV CAXTON_LISTEN=:8080
ENV CAXTON_LIBRARY=/books
ENV CAXTON_DATABASE=/data/caxton.db
ENV CAXTON_COVER_CACHE=/data/covers
ENV CAXTON_SCAN_INTERVAL=5m

EXPOSE 8080

VOLUME ["/books", "/data"]

ENTRYPOINT ["/app/caxton"]
