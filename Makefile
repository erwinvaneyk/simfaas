REPO=docker.io/erwinvaneyk
IMAGE=simfission
VERSION=latest

.PHONY: publish test

build: simfaas.go cmd/simfission/simfission.go Dockerfile
	docker build --tag="${REPO}/${IMAGE}:${VERSION}" .

publish: build
	docker push "${REPO}/${IMAGE}:${VERSION}"

serve: build
	docker run --rm -p 8888:8888 "${REPO}/${IMAGE}:${VERSION}"