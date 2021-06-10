FROM golang:alpine AS builder
ENV GO111MODULE on
RUN apk add --no-cache git
WORKDIR $GOPATH/src/github.com/CyCoreSystems/dispatchers
COPY . .
RUN go get -d -v
RUN go build -o /go/bin/app

FROM alpine
RUN apk add --no-cache ca-certificates
COPY --from=builder /go/bin/app /go/bin/app
ENTRYPOINT ["/go/bin/app"]
