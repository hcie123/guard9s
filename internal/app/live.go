package app

import (
	"context"
	"time"

	"github.com/hcie123/guard9s/internal/kubernetes"
	"github.com/hcie123/guard9s/internal/ui"
)

func liveSource(ctx context.Context, path, contextName, namespace string, timeout time.Duration) (ui.Source, error) {
	client, name, err := kubernetes.Client(path, contextName)
	if err != nil {
		return nil, err
	}
	source := kubernetes.NewSource(client, name, namespace)
	source.KubeconfigPath = path
	if err := source.Start(ctx, timeout); err != nil {
		return nil, err
	}
	return source, nil
}
