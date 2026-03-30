FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY services/gateway/go.mod services/gateway/go.sum /src/services/gateway/
WORKDIR /src/services/gateway
RUN go mod download

COPY services/gateway /src/services/gateway
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/gateway .

FROM alpine:3.20

WORKDIR /app
COPY --from=builder /out/gateway /app/gateway

EXPOSE 8080

ENTRYPOINT ["/app/gateway"]
