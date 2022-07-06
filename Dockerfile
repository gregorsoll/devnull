FROM golang:1.17.6-alpine AS build
WORKDIR /src
COPY . .
RUN go mod download && go build -o /out/devnull .
FROM alpine:3.15 AS bin
COPY --from=build /out/devnull /app/
COPY --from=build /src/config.yaml /app/
WORKDIR /app

EXPOSE 8080

ENTRYPOINT /app/devnull
