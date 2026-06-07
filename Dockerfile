FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Pure-Go SQLite (modernc.org/sqlite) means no CGO and no C toolchain:
# the resulting binary is fully static.
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/web-req-attr ./cmd/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app

COPY --from=builder /app/web-req-attr .
COPY config.example.json ./config.json

EXPOSE 8080
VOLUME ["/app/data", "/var/log/nginx"]

ENTRYPOINT ["./web-req-attr"]
CMD ["-config", "config.json"]
