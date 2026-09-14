FROM golang:1.26.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/bot ./cmd/bot

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/bot /app/bot
USER app
EXPOSE 8080
ENTRYPOINT ["/app/bot"]
