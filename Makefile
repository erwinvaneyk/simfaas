REPO=docker.io/erwinvaneyk
IMAGE=simfission
VERSION=latest

.PHONY: build publish test

build: cmd/simfission/simfission.go
	(cd cmd/simfission && docker build --tag="${REPO}/${IMAGE}:${VERSION}" .)

publish: build
	docker push "${REPO}/${IMAGE}:${VERSION}"

serve: build
	docker run --rm -p 8080:80 "${REPO}/${IMAGE}:${VERSION}"