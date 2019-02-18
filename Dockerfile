FROM golang:1.11.0 AS builder

WORKDIR /go/src/github.com/erwinvaneyk/simfaas

COPY . .
# Future: Speed up builds by also copying the mod cache ($GOPATH/pkg/mod)

ENV GO111MODULE=on
RUN go get
WORKDIR /go/src/github.com/erwinvaneyk/simfaas/cmd/simfission
RUN CGO_ENABLED=0 go build \
    -gcflags=-trimpath="/go/src/github.com/erwinvaneyk/simfaas" \
    -asmflags=-trimpath="/go/src/github.com/erwinvaneyk/simfaas" \
    -ldflags "-X \"main.buildTime=$(date)\"" \
    -v \
    -o /simfission

FROM scratch

COPY --from=builder /simfission /simfission

ENTRYPOINT ["/simfission"]