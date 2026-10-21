# syntax=docker/dockerfile:1

# Imagen de produccion. Se construye en GitHub Actions y se publica en GHCR: el
# servidor nunca compila, solo descarga.

FROM golang:1.27-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.22 AS runner
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=builder /out/api /app/api
USER app
ENV PORT=3001 APP_ENV=production
EXPOSE 3001
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/app/api", "healthcheck"]
ENTRYPOINT ["/app/api"]
