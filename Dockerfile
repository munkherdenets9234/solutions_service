# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy all source first so go mod tidy can resolve imports
COPY . .

# Generate go.sum and download dependencies inside the container
RUN go mod tidy

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server ./cmd/api

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app

COPY --from=builder /app/server .

EXPOSE 8080

CMD ["./server"]
