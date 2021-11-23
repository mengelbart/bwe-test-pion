FROM golang:1.17 as builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY answer/main.go ./

RUN go build -o /answer


FROM engelbart/endpoint:latest

COPY answer/run.sh .

RUN chmod +x run.sh

COPY --from=builder /answer .

ENTRYPOINT ["/run.sh"]

