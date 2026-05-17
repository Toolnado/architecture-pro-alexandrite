FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod .
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /app/service .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/service ./service
EXPOSE 8080
CMD ["./service"]
