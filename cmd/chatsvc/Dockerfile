# Dockerfile
FROM golang:1.25-alpine

WORKDIR /app 

COPY go.mod go.sum ./
RUN go mod download

COPY . . 

RUN go build -o chatserver cmd/chatsvc/main.go

EXPOSE 50052

CMD ["./chatserver"]
