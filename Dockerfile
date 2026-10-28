FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
COPY go.mod go.sum main.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-X main.version=${VERSION}" -o /out/vps-jsq .

FROM alpine:3.21
WORKDIR /app
RUN apk add --no-cache ca-certificates && mkdir -p /data
COPY --from=build /out/vps-jsq /app/vps-jsq
COPY index.html /app/
COPY static /app/static
ENV PORT=8080 STATIC_DIR=/app
EXPOSE 8080
ENTRYPOINT ["/app/vps-jsq"]
