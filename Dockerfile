FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /replicant ./cmd/replicant/

FROM alpine:3.21

RUN apk add --no-cache \
    git \
    ripgrep \
    bash \
    ca-certificates

COPY --from=builder /replicant /usr/local/bin/replicant
COPY replicants/ /etc/replicant/replicants/

ENV REPLICANT_REPLICANTS_DIR=/etc/replicant/replicants

ENTRYPOINT ["replicant"]
