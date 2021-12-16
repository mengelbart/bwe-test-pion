FROM golang:1.17 as builder

WORKDIR /src

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN go build -o bwe-test-pion

FROM engelbart/endpoint:latest

COPY --from=builder \
	/src/bwe-test-pion \
	/src/tools/run_endpoint.sh \
	./

RUN chmod +x run_endpoint.sh

ENTRYPOINT [ "./run_endpoint.sh" ]

