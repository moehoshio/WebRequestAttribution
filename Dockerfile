FROM golang:1.22-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o /app/web-req-attr ./cmd/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates sqlite-libs
WORKDIR /app

COPY --from=builder /app/web-req-attr .
COPY config.example.json ./config.json

EXPOSE 8080
VOLUME ["/app/data", "/var/log/nginx"]

ENTRYPOINT ["./web-req-attr"]
CMD ["-config", "config.json"]
