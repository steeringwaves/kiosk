FROM golang:1.24-alpine

# Install tools
RUN apk add --no-cache make

WORKDIR /app

# Copy source and Makefile
COPY internal/ ./internal/
COPY cmd/ ./cmd/
COPY go.mod ./
COPY go.sum ./
COPY Makefile ./

# Build the binary using Makefile
RUN make kiosk
