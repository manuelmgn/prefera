# Stage 1: Compile Go binary
FROM golang:1.22-alpine AS builder

WORKDIR /build

COPY go.mod go.sum* ./
RUN go mod download

# Copy all source code (templates/static are embedded via go:embed)
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o proj_listas .

# Stage 2: Minimal final image
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Templates, static files and dominios.txt are embedded in the binary
COPY --from=builder /build/proj_listas .

# Directory for the local SQLite database
RUN mkdir -p /app/data

ENV DB_PATH=/app/data/listas.db

EXPOSE 7010

CMD ["./proj_listas"]
