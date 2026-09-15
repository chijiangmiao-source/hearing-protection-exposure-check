# syntax=docker/dockerfile:1
FROM node:20-alpine AS build
WORKDIR /app
# 镜像内只构建前端，不下载 Playwright 浏览器。
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=5s --timeout=3s --start-period=2s --retries=10 \
  CMD wget -qO- http://127.0.0.1/health > /dev/null || exit 1
