FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/vps-jsq .

FROM alpine:3.21
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=build /out/vps-jsq /app/vps-jsq
COPY index.html /app/
COPY static /app/static
ENV PORT=8080 STATIC_DIR=/app
EXPOSE 8080
USER 10001:10001
ENTRYPOINT ["/app/vps-jsq"]
