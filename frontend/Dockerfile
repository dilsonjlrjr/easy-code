# syntax=docker/dockerfile:1

# Build stage: install pinned dependencies and produce the static bundle.
FROM node:24.22.3-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
RUN npm run build

# Runtime stage: unprivileged nginx serving static files only.
FROM nginxinc/nginx-unprivileged:1.31.7-alpine AS runtime
ENV API_BASE_URL=/api/v1
ENV DEV_SIGN_IN=false
COPY docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY --chmod=0755 docker/40-write-runtime-config.sh /docker-entrypoint.d/40-write-runtime-config.sh
COPY --from=build --chown=nginx:nginx /app/dist /usr/share/nginx/html
EXPOSE 8080
