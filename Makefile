.PHONY: all build push lint

TAG=latest
CR=icn.vultrcr.com/homincr1
IMAGE_TAG=${CR}/concierge:${TAG} 

all: lint push

build:
	docker buildx build --platform linux/amd64 -t ${IMAGE_TAG} .

push: build
	docker push ${IMAGE_TAG}

lint:
	golangci-lint run ./...
