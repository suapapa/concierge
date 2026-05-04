.PHONY: all build push lint swagger

TAG=latest
CR=icn.vultrcr.com/homincr1
IMAGE_TAG=${CR}/concierge:${TAG} 

all: lint push

build:
	docker buildx build --platform linux/amd64 -t ${IMAGE_TAG} .

push: swagger build
	docker push ${IMAGE_TAG}

lint:
	golangci-lint run ./...

swagger:
	@which swag > /dev/null || (echo "swag not found. Install with: go install github.com/swaggo/swag/cmd/swag@latest" && exit 1)
	swag init -g main.go -o docs --parseInternal
