FROM golang:1.22-alpine AS build
WORKDIR /app
COPY . .
RUN apk add --no-cache build-base git
RUN go build -o /out/proxy ./cmd/proxy

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/proxy /usr/local/bin/proxy
COPY internal/store/postgres_schema.sql internal/store/postgres_schema.sql
ENV HTTP_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/proxy"]


