FROM golang:1.17 as builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY offer/main.go ./

RUN go build -o /offer


FROM engelbart/endpoint:latest

COPY offer/run.sh .

RUN chmod +x run.sh

COPY --from=builder /offer .

ENTRYPOINT ["/run.sh"]

