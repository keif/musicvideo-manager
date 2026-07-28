FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /musicvideo-manager ./cmd/musicvideo-manager

FROM alpine:3.20
RUN apk add --no-cache ffmpeg
COPY --from=builder /musicvideo-manager /usr/local/bin/musicvideo-manager
ENTRYPOINT ["musicvideo-manager"]
CMD ["--help"]
