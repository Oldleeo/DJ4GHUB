FROM golang:1.26.3-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /relay ./cmd/dj4ghub-relay

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -S relay && adduser -S -G relay -u 10001 relay \
    && mkdir -p /data && chown relay:relay /data
COPY --from=build /relay /usr/local/bin/dj4ghub-relay
USER relay
VOLUME ["/data"]
EXPOSE 18444
ENTRYPOINT ["dj4ghub-relay"]
CMD ["-http-only", "-listen", "0.0.0.0:18444", "-data-dir", "/data"]
