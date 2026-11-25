FROM node:24-alpine AS web
WORKDIR /src/web
# Optional package mirrors for slow networks; empty means upstream.
ARG NPM_REGISTRY=
COPY web/package.json web/package-lock.json ./
RUN npm ci ${NPM_REGISTRY:+--registry=$NPM_REGISTRY}
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=$GOPROXY
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /keine ./cmd/keine

FROM alpine:3.22
ARG APK_MIRROR=
RUN if [ -n "$APK_MIRROR" ]; then sed -i "s#https\?://dl-cdn.alpinelinux.org#$APK_MIRROR#" /etc/apk/repositories; fi \
 && apk add --no-cache ca-certificates && adduser -D -H -u 10001 keine && mkdir -p /data && chown keine /data
COPY --from=build /keine /usr/local/bin/keine
USER keine
ENV KEINE_ADDR=:8080 KEINE_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["keine"]
