# Frontend (Vite) production bundle
FROM node:24-alpine AS fe
WORKDIR /app/fe
COPY fe/package.json fe/package-lock.json ./
RUN npm ci
COPY fe/ ./
RUN npm run build

# Go binary + Swagger
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

RUN go install github.com/swaggo/swag/cmd/swag@latest

COPY . .
COPY --from=fe /app/fe/dist ./fe/dist

RUN swag init -g main.go -o docs --parseInternal

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o concierge .

# Runtime
FROM alpine:latest

RUN apk --no-cache add ca-certificates su-exec && \
    addgroup -S appgroup && \
    adduser -S appuser -G appgroup

WORKDIR /app

COPY --from=builder /app/concierge .
COPY --from=builder /app/fe/dist ./fe/dist
COPY docker-entrypoint.sh /docker-entrypoint.sh

RUN chown -R appuser:appgroup /app && chmod +x /docker-entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["./concierge"]
