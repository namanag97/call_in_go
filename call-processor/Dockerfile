# Build stage
FROM golang:1.18-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o call-processor ./cmd/service

# Final stage
FROM alpine:3.16

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/call-processor .

# Copy necessary files
COPY config/default.env .env

# Create a non-root user and change ownership
RUN adduser -D -g '' appuser && \
    chown -R appuser:appuser /app

# Use the non-root user
USER appuser

# Expose the API port
EXPOSE 8080

# Define entrypoint
CMD ["./call-processor"]