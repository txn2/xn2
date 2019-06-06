FROM golang:1.12.5-alpine3.9 AS builder

RUN apk update \
 && apk add git

WORKDIR /go/src

ADD . .

RUN GO111MODULE=on GOPROXY=https://gocenter.io CGO_ENABLED=0 go build -tags=jsoniter -a -installsuffix cgo -o /go/bin/xn2 ./cmd/xn2.go

FROM scratch

COPY --from=builder /go/bin/xn2 /bin/xn2

WORKDIR /

ENTRYPOINT ["/bin/xn2"]
