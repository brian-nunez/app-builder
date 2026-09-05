package execution

import (
	"context"
	"fmt"
	"github.com/brian-nunez/app-builder/internal/platform"
	"github.com/brian-nunez/app-builder/sdk/go/plugin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"sync"
)

// just an example to map out the structure
