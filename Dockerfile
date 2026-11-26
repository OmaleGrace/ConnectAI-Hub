# Build:  docker build -t connectai-hub .
# Run:    docker run --env-file .env -p 8080:8080 connectai-hub
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/connectai-hub .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
WORKDIR /app
# The server reads templates/ and static/ from its working directory.
COPY --from=build /out/connectai-hub .
COPY templates ./templates
COPY static ./static
USER app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:8080/health || exit 1
CMD ["./connectai-hub"]
