FROM golang:1.19-alpine

WORKDIR /app

RUN mkdir data
RUN mkdir data/dem

COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY . ./

RUN go build -o lukla main.go

CMD [ "./lukla", "rest" ]