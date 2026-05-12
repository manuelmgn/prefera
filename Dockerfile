# Etapa 1: Compilar o binário Go
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Copiar todo o código fonte
COPY . .

# Resolver dependências e compilar
# go mod tidy descarrega e gera o go.sum automaticamente
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -o proj_listas .

# Etapa 2: Imagem final mínima (~5MB base)
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copiar o binário compilado
COPY --from=builder /build/proj_listas .

# Copiar templates e ficheiros estáticos
COPY --from=builder /build/templates ./templates
COPY --from=builder /build/static ./static

# Criar directório para a base de dados
RUN mkdir -p /app/data

# Variáveis de ambiente
ENV DB_PATH=/app/data/listas.db
ENV TMPL_PATH=/app/templates
ENV STATIC_PATH=/app/static

EXPOSE 7010

CMD ["./proj_listas"]
