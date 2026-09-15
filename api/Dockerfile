# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
# 纯标准库、静态链接，运行镜像无需任何额外依赖。
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/earmuff-api .

FROM alpine:3.20
RUN adduser -D -u 10001 appuser
COPY --from=build /out/earmuff-api /usr/local/bin/earmuff-api
USER appuser
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=3s --start-period=2s --retries=10 \
  CMD wget -qO- http://127.0.0.1:8080/health || exit 1
ENTRYPOINT ["/usr/local/bin/earmuff-api"]
