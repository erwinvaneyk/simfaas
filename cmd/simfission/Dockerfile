FROM golang:1.11.0 AS builder

WORKDIR /go/src/github.com/erwinvaneyk/simfission

COPY . .

RUN go get
RUN CGO_ENABLED=0 go build \
    -gcflags=-trimpath="/go/src/github.com/erwinvaneyk/simfaas" \
    -asmflags=-trimpath="/go/src/github.com/erwinvaneyk/simfaas" \
    -ldflags "-X \"main.buildTime=$(date)\"" \
    -o simfission

FROM scratch

COPY --from=builder /go/src/github.com/erwinvaneyk/simfission/simfission /simfission

ENTRYPOINT ["/simfission"]