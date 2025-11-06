# Build stage
FROM golang:1.25-alpine AS builder

# 작업 디렉터리 설정
WORKDIR /app

# 의존성 파일 복사
COPY go.mod go.sum ./

# 의존성 다운로드
RUN go mod download

# swag 설치 (swagger 문서 생성용)
RUN go install github.com/swaggo/swag/cmd/swag@latest

# 소스 코드 복사
COPY go.mod go.sum ./
COPY *.go ./

# Swagger 문서 생성
RUN swag init -g main.go -o docs

# 바이너리 빌드
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o concierge .

# Runtime stage
FROM alpine:latest

# 보안을 위한 non-root 사용자 추가
RUN apk --no-cache add ca-certificates && \
    addgroup -S appgroup && \
    adduser -S appuser -G appgroup

WORKDIR /app

# 빌더에서 바이너리 복사
COPY --from=builder /app/concierge .

# 소유권 변경
RUN chown -R appuser:appgroup /app

# non-root 사용자로 전환
USER appuser

# 포트 노출
EXPOSE 8080

# 애플리케이션 실행
CMD ["./concierge"]

