FROM golang:1.26-alpine AS builder
WORKDIR /app
# go.mod requires go 1.26.2; auto-download toolchain if base image differs slightly
ENV GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o gateway ./cmd/gateway

FROM alpine:3.19
RUN apk --no-cache add ca-certificates wget
COPY --from=builder /app/gateway /gateway
COPY config.yaml /config.yaml
EXPOSE 8080
CMD ["/gateway", "--config=/config.yaml"]
