package config

import (
	"context"
	"github.com/brian-nunez/bconfig"
	"github.com/brian-nunez/bconfig/drivers/file"
	"os"
	"strconv"
	"strings"
)

type environment struct{}

func (environment) Name() string { return "workflow-environment" }
func (environment) Load(ctx context.Context) (map[string]any, error) {
	result := map[string]any{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(key, "APP__") {
			continue
		}
		parts := strings.Split(strings.ToLower(strings.TrimPrefix(key, "APP__")), "__")
		node := result
		for _, part := range parts[:len(parts)-1] {
			next, ok := node[part].(map[string]any)
			if !ok {
				next = map[string]any{}
				node[part] = next
			}
			node = next
		}
		var v any = value
		if value == "true" || value == "false" {
			v = value == "true"
		} else if parts[len(parts)-1] == "port" {
			if number, err := strconv.Atoi(value); err == nil {
				v = number
			}
		}
		node[parts[len(parts)-1]] = v
	}
	return result, nil
}
func Load(ctx context.Context) (*bconfig.Config, error) {
	return bconfig.Load(ctx, file.Source("config.yaml"), environment{})
}
