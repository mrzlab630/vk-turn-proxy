FROM golang:1.25-alpine AS builder

WORKDIR /build

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o vk-turn-proxy-server ./server

FROM alpine:3.23

WORKDIR /app

COPY docker-entrypoint.sh .
COPY --from=builder /build/vk-turn-proxy-server .
RUN chmod +x docker-entrypoint.sh \
    && ln -s /app/vk-turn-proxy-server /app/vk-turn-proxy

EXPOSE 56000/udp

ENTRYPOINT ["./docker-entrypoint.sh"]
