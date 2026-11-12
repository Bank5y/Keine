FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /keine ./cmd/keine

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 keine && mkdir -p /data && chown keine /data
COPY --from=build /keine /usr/local/bin/keine
USER keine
ENV KEINE_ADDR=:8080 KEINE_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["keine"]
