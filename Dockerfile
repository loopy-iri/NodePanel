# Control-plane panel image. Web assets are embedded via go:embed, so the final
# image is a single static binary.
FROM golang:1.26.2-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/panel ./cmd/panel

FROM alpine:latest

LABEL org.opencontainers.image.title="pasarguard-panel"
LABEL org.opencontainers.image.description="Multi-tenant node-selling control plane"

RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/panel /app/panel

ENTRYPOINT ["/app/panel"]
