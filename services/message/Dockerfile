FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY services/message/go.mod /src/services/message/go.mod
WORKDIR /src/services/message
RUN go mod download

COPY services/message /src/services/message
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/message .

FROM alpine:3.20

WORKDIR /app
COPY --from=builder /out/message /app/message

EXPOSE 9003

ENTRYPOINT ["/app/message"]
