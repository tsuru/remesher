FROM golang:1.9.0 as builder
COPY . /go/src/github.com/tsuru/remesher/
WORKDIR /go/src/github.com/tsuru/remesher/
RUN CGO_ENABLED=0 GOOS=linux make build

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /go/src/github.com/tsuru/remesher/remesher .
CMD ["./remesher"]
