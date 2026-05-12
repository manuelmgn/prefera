# Stage 1: Compile Go binary
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Copy all source code
COPY . .

# Resolve dependencies and compile
# go mod tidy downloads and generates go.sum automatically
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -o proj_listas .

# Stage 2: Minimal final image (~5MB base)
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy compiled binary
COPY --from=builder /build/proj_listas .

# Copy templates and static files
COPY --from=builder /build/templates ./templates
COPY --from=builder /build/static ./static

# Create directory for database
RUN mkdir -p /app/data

# Environment variables
ENV DB_PATH=/app/data/listas.db
ENV TMPL_PATH=/app/templates
ENV STATIC_PATH=/app/static

EXPOSE 7010

CMD ["./proj_listas"]
