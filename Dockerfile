# Use the official golang image as a base
FROM golang:1.21 AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy the Go modules manifests
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

RUN go mod tidy

# Copy the source code and the credentials file into the container
COPY . .

# Build the Go application
RUN CGO_ENABLED=0 GOOS=linux go build -o mender-usercreate .

# Use a minimal base image for the runtime
FROM alpine:latest

# Install ca-certificates to ensure HTTPS works consistently
RUN apk add --no-cache ca-certificates

# Set the working directory inside the container
WORKDIR /root/

# Copy the built executable from the builder stage
COPY --from=builder /app/mender-usercreate .


# Expose the port that your application listens on (if any)
# Update the port according to your application's requirements
# EXPOSE 4222 is usually for NATS server; adjust if your application provides an HTTP/other service interface
EXPOSE 8080

# Command to run the executable
CMD ["./mender-usercreate"]
