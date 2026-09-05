FROM golang:1.25.7-alpine3.23 AS build
WORKDIR /src
COPY cmd/healthcheck/main.go ./main.go
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /healthcheck main.go
FROM otel/opentelemetry-collector-contrib:0.146.1
COPY --from=build /healthcheck /healthcheck
USER 10001:10001
LABEL maintainer="Brian Nunez" version="0.1.0" description="Workflow OpenTelemetry Collector" platform="workflow"
