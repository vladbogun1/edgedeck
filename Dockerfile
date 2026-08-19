# Образ EdgeDeck: интерфейс собирается vite и вшивается в бинарник Go,
# поэтому на выходе один статический файл без рантайма и без nginx рядом.
#
# Мультиплатформенность сделана кросс-компиляцией, а не QEMU: --platform=$BUILDPLATFORM
# у сборочных стадий + GOARCH из TARGETARCH. Сборка arm64 (все три сервера на Ampere)
# идёт с той же скоростью, что amd64.

FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
# Заглушка из репозитория заменяется настоящей сборкой интерфейса.
RUN rm -rf internal/webui/dist
COPY --from=web /web/dist ./internal/webui/dist
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags "-s -w -X github.com/vladbogun1/edgedeck/internal/version.Version=${VERSION}" \
      -o /out/edgedeck ./cmd/edgedeck

FROM alpine:3.21
# iptables нужен только режиму --firewall: он читает цепочки DOCKER-USER и INPUT,
# чтобы панель могла честно сказать, видно ли порт из интернета.
RUN apk add --no-cache ca-certificates tzdata iptables && \
    mkdir -p /data /state
COPY --from=build /out/edgedeck /usr/local/bin/edgedeck
ENV EDGEDECK_LISTEN=:4000
# Порт не публикуется наружу: снаружи в панель попадают только через Caddy.
EXPOSE 4000
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s \
    CMD wget -qO- http://127.0.0.1:4000/healthz >/dev/null 2>&1 || exit 1
ENTRYPOINT ["/usr/local/bin/edgedeck"]
