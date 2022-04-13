FROM golang:latest AS builder

WORKDIR /app
COPY go.mod go.sum /app/
RUN go mod download
COPY main.go /app/
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/dovemon .

FROM alpine:latest
COPY --from=builder /app/dovemon /dovemon
RUN chmod +x /dovemon

ENTRYPOINT ["/dovemon"]