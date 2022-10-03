FROM golang:1.16-alpine

# Required because go requires gcc to build
RUN apk add build-base

RUN apk add inotify-tools

RUN echo $GOPATH

COPY . /galene

ENV GOPROXY="https://goproxy.cn,direct"

WORKDIR /galene


RUN --mount=type=cache,mode=0755,target=/go/pkg/mod go mod download -x -json
RUN --mount=type=cache,mode=0755,target=/go/pkg/mod go mod vendor


RUN CGO_ENABLED=0 go build -ldflags='-s -w'
CMD ./galene &
