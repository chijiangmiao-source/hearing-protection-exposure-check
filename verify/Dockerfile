# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/verify .

FROM alpine:3.20
# verify 需要以 HTTP 探测 api/web；wget 不是必需的（程序内置 net/http），保持最小镜像即可。
COPY --from=build /out/verify /verify
# 一次性验收服务：跑完即退出，restart: "no" 由 compose 保证。
ENTRYPOINT ["/verify"]
