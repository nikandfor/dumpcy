FROM golang:1.25 AS builder

ENV GOWORK=off
ENV CGO_ENABLED=0

WORKDIR /app

COPY go.mod go.sum main.go meter.go ./

RUN go build -o app .

FROM alpine:latest

COPY --from=builder /app/app /bin/dumpcy

ENTRYPOINT ["/bin/dumpcy"]
