FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o quotaguard ./cmd/quotaguard

FROM alpine:3.19
RUN apk --no-cache add ca-certificates

RUN addgroup -g 1000 appgroup && \
    adduser -u 1000 -G appgroup -s /bin/sh -D appuser

WORKDIR /app
COPY --from=builder /app/quotaguard .
COPY --from=builder /app/config.yaml ./config.yaml

# Handle SIGTERM for graceful shutdown
ENV SHUTDOWN_TIMEOUT=25s

USER appuser
EXPOSE 8318

# Docker sends SIGTERM, then waits for graceful shutdown
CMD ["sh", "-c", "trap 'kill -TERM $!' TERM; ./quotaguard serve --config config.yaml & wait"]
