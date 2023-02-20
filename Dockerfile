FROM golang:1.18-alpine

WORKDIR /app

RUN mkdir data
RUN mkdir data/dem

COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY internal/ internal/
COPY *.go ./

RUN go build -o lukla main.go

EXPOSE 9000

CMD [ "./lukla" ]