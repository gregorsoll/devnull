FROM golang:1.17.6-alpine AS build
WORKDIR /src
COPY . .
RUN go mod download && go build -o /out/app .
FROM alpine:3.15 AS bin
COPY --from=build /out/app /app/
COPY --from=build /src/config.yaml /app/
WORKDIR /app

EXPOSE 8080
#EXPOSE 53
#EXPOSE 12201/udp

ENTRYPOINT /app/app