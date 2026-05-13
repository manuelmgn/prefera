# Stage 1: Compile Go binary
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Copy all source code
COPY . .

# Resolve dependencies and compile
# go mod tidy downloads and generates go.sum automatically
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -o prefera .

# Stage 2: Minimal final image (~5MB base)
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy compiled binary
COPY --from=builder /build/prefera .

# Copy templates and static files
COPY --from=builder /build/templates ./templates
COPY --from=builder /build/static ./static

# Create directories for database and config
RUN mkdir -p /app/data /data

# Environment variables (can be overridden by Railway)
ENV DB_PATH=/data/listas.db
ENV TMPL_PATH=/app/templates
ENV STATIC_PATH=/app/static

# Dynamic port assigned by Railway (default to 7010 for local development)
EXPOSE 3000

CMD ["./prefera"]
