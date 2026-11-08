FROM node:24-alpine AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.26-alpine AS api
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/cmd ./cmd
COPY backend/internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /hookymaster ./cmd/hookymaster

FROM alpine:3.22
RUN adduser -D -H app
WORKDIR /app
COPY --from=api /hookymaster /app/hookymaster
COPY --from=web /web/dist /app/web
COPY testdata /app/testdata
ENV WEB_DIR=/app/web
USER app
EXPOSE 8080
ENTRYPOINT ["/app/hookymaster"]
