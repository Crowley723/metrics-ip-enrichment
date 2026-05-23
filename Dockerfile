FROM golang:1.26.2-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app/metrics-ip-enrichment .


FROM alpine:3.23

RUN addgroup -S app && adduser -S -G app app

WORKDIR /app
COPY --from=builder /app/metrics-ip-enrichment .

USER app

EXPOSE 8080

ENTRYPOINT ["/app/metrics-ip-enrichment", "-c", "/etc/metrics-ip-enrichment/config.yaml"]
