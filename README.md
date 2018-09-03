# Centrifugo Bench

## Installation

```
go get -u github.com/badoo/centrifugo-bench
```

## Build
```
docker run --rm -v $GOPATH:/go -e "GOPATH=/go" -w /go/src/github.com/badoo/centrifugo-bench golang:1.9 go build -v
```

## Usage

```
Usage of centrifugo-bench:
  -channel-rps uint
    	Message per second for channel (default 1)
  -channels uint
    	Channels count. (default 1)
  -clients-per-channel uint
    	Clients per channel count. (default 1)
  -connection-concurrency uint
    	Max concurrency for establishing connections (default 10)
  -secret string
    	Secret.
  -url string
    	WS URL, e.g.: ws://localhost:8000/connection/websocket.
```
