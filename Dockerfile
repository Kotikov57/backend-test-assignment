FROM golang:1.22 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api

FROM debian:bookworm-slim

WORKDIR /app
COPY --from=builder /out/api /app/api
COPY migrations /app/migrations

EXPOSE 8080

CMD ["/app/api"]
