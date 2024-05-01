# Use the official golang image as a base
FROM golang:1.21 AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy the Go modules manifests
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Optionally tidy up the dependencies
RUN go mod tidy

# Copy the source code into the container
COPY . .

# Copy the NATS credentials file
COPY NGS-Karthick-karthick.creds ./

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

# Copy the NATS credentials file from the builder stage
COPY --from=builder /app/NGS-Karthick-karthick.creds .

# Expose the port that your application listens on (if any)
# Update the port according to your application's requirements
# EXPOSE 4222 is usually for NATS server; adjust if your application provides an HTTP/other service interface
EXPOSE 4222

# Command to run the executable
CMD ["./mender-usercreate"]
