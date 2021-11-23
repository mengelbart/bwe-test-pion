FROM golang:1.17

WORKDIR /app

COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY offer/main.go ./

RUN go build -o /offer

CMD ["/offer"]

