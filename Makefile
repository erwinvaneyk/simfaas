REPO=docker.io/erwinvaneyk
IMAGE=simfission
VERSION=latest
ROOT_DIR=$(dirname $0)

.PHONY: publish test build install

build:
	CGO_ENABLED=0 go build \
        -gcflags=-trimpath="/go/src/github.com/erwinvaneyk/simfaas" \
        -asmflags=-trimpath="/go/src/github.com/erwinvaneyk/simfaas" \
        -ldflags "-X \"main.buildTime=$(date)\" " \
		 $(CURDIR)/cmd/simfission

install:
	CGO_ENABLED=0 go install \
        -gcflags=-trimpath="/go/src/github.com/erwinvaneyk/simfaas" \
        -asmflags=-trimpath="/go/src/github.com/erwinvaneyk/simfaas" \
        -ldflags "-X \"main.buildTime=$(date)\" " \
        $(CURDIR)/cmd/simfission


docker-build: simfaas.go cmd/simfission/simfission.go Dockerfile
	docker build --tag="${REPO}/${IMAGE}:${VERSION}" .


publish: docker-build
	docker push "${REPO}/${IMAGE}:${VERSION}"

serve: docker-build
	docker run --rm -p 8888:8888 "${REPO}/${IMAGE}:${VERSION}"