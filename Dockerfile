FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/cdn-edge ./cmd

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/cdn-edge /usr/local/bin/cdn-edge
COPY configs/edge-config.yaml /etc/cdn-edge/config.yaml
EXPOSE 8080 8443
CMD ["cdn-edge", "-config", "/etc/cdn-edge/config.yaml"]
