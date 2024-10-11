
FROM golang:1.21 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

RUN go mod tidy

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o mender-usercreate .

FROM alpine:latest


RUN apk add --no-cache ca-certificates

WORKDIR /root/

COPY --from=builder /app/mender-usercreate .

EXPOSE 4222

CMD ["./mender-usercreate"]
