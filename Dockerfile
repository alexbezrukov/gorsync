FROM golang:1.24 AS build

WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o gorsync gorsync/cmd

FROM debian:bookworm
WORKDIR /app
COPY --from=build /app/gorsync .
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

ENTRYPOINT ["/app/entrypoint.sh"]
