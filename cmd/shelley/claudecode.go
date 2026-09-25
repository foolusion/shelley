package main

import (
	"context"
	"log/slog"
	"net/http"

	"shelley.exe.dev/models"
	"shelley.exe.dev/modelsources"
)

// buildModels is modelsources.Build plus the local Claude Code models.
func buildModels(ctx context.Context, sources []modelsources.Source, httpc *http.Client, logger *slog.Logger) ([]models.Built, error) {
	cc, err := modelsources.ClaudeCode(ctx)
	if err != nil {
		return nil, err
	}
	return append(modelsources.Build(models.All(), sources, httpc, logger), cc...), nil
}
