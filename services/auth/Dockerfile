FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY services/auth/go.mod services/auth/go.sum /src/services/auth/
WORKDIR /src/services/auth
RUN go mod download

COPY services/auth /src/services/auth
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/auth .

FROM alpine:3.20

WORKDIR /app
COPY --from=builder /out/auth /app/auth

EXPOSE 9001

ENTRYPOINT ["/app/auth"]
