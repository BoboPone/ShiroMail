FROM docker.io/library/node:22-alpine AS frontend-builder

WORKDIR /frontend
ARG VITE_API_BASE_URL=
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM docker.io/library/golang:1.24-alpine AS backend-builder

RUN apk add --no-cache git

WORKDIR /backend
COPY backend/go.mod backend/go.sum ./
COPY backend/vendor ./vendor
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-s -w" -o /bin/shiro-api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-s -w" -o /bin/shiro-worker ./cmd/worker

FROM docker.io/library/alpine:3.20

RUN apk add --no-cache ca-certificates tzdata nginx
RUN adduser -D -u 1000 shiro
ENV GIN_MODE=release

COPY --from=backend-builder /bin/shiro-api /usr/local/bin/shiro-api
COPY --from=backend-builder /bin/shiro-worker /usr/local/bin/shiro-worker
COPY --from=frontend-builder /frontend/dist /usr/share/nginx/html
COPY frontend/nginx.conf /etc/nginx/http.d/default.conf
COPY docker/start-app.sh /usr/local/bin/start-app

RUN sed -i 's/\r$//' /usr/local/bin/start-app \
    && chmod +x /usr/local/bin/start-app \
    && sed -i '/^user /d' /etc/nginx/nginx.conf \
    && mkdir -p /app/data/mail /run/nginx /var/lib/nginx/tmp /var/log/nginx \
    && chown -R shiro:shiro /app /run/nginx /var/lib/nginx /var/log/nginx /usr/share/nginx/html

USER shiro
WORKDIR /app

EXPOSE 80 8080 2525

CMD ["/usr/local/bin/start-app"]
