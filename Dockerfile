FROM golang:1.19-alpine AS builder

WORKDIR /app

RUN mkdir data
RUN mkdir data/dem

COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY . ./

RUN go build -ldflags="-s -w" -o lukla main.go

FROM golang:1.19-alpine

WORKDIR /usr/local/bin

COPY --from=builder /app/lukla .

ENTRYPOINT [ "lukla", "rest" ]