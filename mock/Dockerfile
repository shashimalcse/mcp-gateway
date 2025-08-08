FROM golang:1.22-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/mock ./cmd/mock

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/mock /usr/local/bin/mock
EXPOSE 9090
ENTRYPOINT ["/usr/local/bin/mock"]


