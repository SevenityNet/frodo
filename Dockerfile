FROM golang:1.22-alpine as builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -o frodo .

FROM alpine:latest as runtime

COPY --from=builder /app/frodo /usr/local/bin/frodo

ENTRYPOINT [ "frodo" ]