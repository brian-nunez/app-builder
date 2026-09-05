package telemetry

import (
	"context"
	"github.com/brian-nunez/bconfig"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"log/slog"
)

// BTelemetry initializes traces and metrics; its API does not yet expose logs.
func Logs(ctx context.Context, cfg *bconfig.Config) (func(context.Context) error, error) {
	opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(cfg.String("telemetry.otlp_endpoint"))}
	if cfg.Bool("telemetry.otlp_insecure") {
		opts = append(opts, otlploggrpc.WithInsecure())
	}
	exporter, err := otlploggrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	provider := sdklog.NewLoggerProvider(sdklog.WithResource(resource.NewWithAttributes("", attribute.String("service.name", cfg.String("telemetry.service_name")))), sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)))
	slog.SetDefault(otelslog.NewLogger("workflow.platform", otelslog.WithLoggerProvider(provider)))
	return provider.Shutdown, nil
}
