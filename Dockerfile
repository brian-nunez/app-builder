FROM node:22.22.0-alpine3.23 AS ui
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25.7-alpine3.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY sdk/ sdk/
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/platform ./cmd/api && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/healthcheck ./cmd/healthcheck

FROM alpine:3.23.3
RUN apk add --no-cache ca-certificates && addgroup -g 10001 app && adduser -D -u 10001 -G app app
WORKDIR /app
LABEL maintainer="Brian Nunez" version="0.1.0" description="Workflow platform and Vite UI" platform="workflow"
COPY --from=build /out/ /usr/local/bin/
COPY --from=ui /src/web/dist/ web/dist/
COPY config.yaml ./
COPY plugins/ plugins/
USER app
EXPOSE 8080
ENTRYPOINT ["platform"]
