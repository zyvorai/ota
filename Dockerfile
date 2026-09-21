# syntax=docker/dockerfile:1
# Build container for make dist / make demo. Not a device rootfs.
FROM golang:1.27.1-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends python3 gcc libc6-dev ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY . .
RUN make build && make dist
