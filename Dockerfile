FROM golang:1.25.5-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -o papaya .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/papaya /app/papaya
RUN apk add --no-cache ca-certificates
VOLUME ["/app/data"]
ENV DATA_FILE=/app/data/papaya.db
ENTRYPOINT ["/app/papaya"]
