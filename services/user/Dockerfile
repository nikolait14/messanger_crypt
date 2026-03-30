FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY services/user/go.mod /src/services/user/go.mod
WORKDIR /src/services/user
RUN go mod download

COPY services/user /src/services/user
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/user .

FROM alpine:3.20

WORKDIR /app
COPY --from=builder /out/user /app/user

EXPOSE 9002

ENTRYPOINT ["/app/user"]
