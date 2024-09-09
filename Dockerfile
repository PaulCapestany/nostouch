# Use an official Golang runtime as a parent image
FROM golang:1.23 AS builder

# Set the working directory inside the container
WORKDIR /app

# Clone the `nak` repository
RUN git clone https://github.com/fiatjaf/nak.git

# Copy the `nostouch` source files into the container
COPY . ./nostouch

# Build the `nak` binary
WORKDIR /app/nak
RUN go mod tidy && go build -o /app/bin/nak .

# Build the `nostouch` binary
WORKDIR /app/nostouch
RUN go mod tidy && go build -o /app/bin/nostouch .

# Use a smaller base image for the final stage
FROM debian:latest

# Install necessary certificates
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

# Copy the built binaries from the builder stage
COPY --from=builder /app/bin/nak /usr/local/bin/nak
COPY --from=builder /app/bin/nostouch /usr/local/bin/nostouch

# Expose any necessary ports (if needed)
EXPOSE 7777

# Command to run both `nak` and `nostouch`
CMD ["sh", "-c", "printf '{\"since\":1725911165}' | nak req ws://strfry.strfry-project.svc.cluster.local:7777 | nostouch -conn couchbase-cluster.default.svc.cluster.local"]