FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/support-ticket-api ./cmd/server

FROM alpine:3.22
RUN addgroup -S app && adduser -S app -G app && mkdir -p /app/data/uploads && chown -R app:app /app
WORKDIR /app
COPY --from=build /out/support-ticket-api /app/support-ticket-api
USER app
EXPOSE 8080
ENTRYPOINT ["/app/support-ticket-api"]
