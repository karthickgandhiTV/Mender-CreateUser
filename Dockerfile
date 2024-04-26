# Use the official golang image as a base
FROM golang:1.21 AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy the Go modules manifests
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

RUN go mod tidy

# Copy the source code into the container
COPY . .

# Build the Go application
RUN CGO_ENABLED=0 GOOS=linux go build -o mender-usercreate .

# Use a minimal base image for the runtime
FROM alpine:latest

# Set the working directory inside the container
WORKDIR /root/

# Copy the built executable from the builder stage
COPY --from=builder /app/mender-usercreate .

# Expose the port that your application listens on (if any)
EXPOSE 8080

# Command to run the executable
CMD ["./mender-usercreate"]
