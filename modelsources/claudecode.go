package modelsources

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"

	directsdk "go.aponeill.com/claude-directsdk"
	"shelley.exe.dev/llm/ant"
	"shelley.exe.dev/models"
)

// claudeCodeOpts runs native on its own login. Shelley's Anthropic credentials
// and base URL are dropped: with them native would bill the API key, or send
// the subscription bearer to another host.
var claudeCodeOpts = directsdk.Options{Env: slices.DeleteFunc(os.Environ(), func(kv string) bool {
	return strings.HasPrefix(kv, "ANTHROPIC_") || strings.HasPrefix(kv, "CLAUDE_CODE_USE_")
})}

// claudeCodeClient is shared so every request runs in one cwd and the cached
// prefix stays stable.
var claudeCodeClient = directsdk.New(claudeCodeOpts)

// ClaudeCode materializes the models the local Claude Code CLI offers its
// logged-in Pro/Max account, as "<model>@claude-code". It returns none when
// claude is not installed or not logged in.
func ClaudeCode(ctx context.Context) ([]models.Built, error) {
	found, err := directsdk.DiscoverModels(ctx, claudeCodeOpts)
	if errors.Is(err, directsdk.ErrClaudeMissing) || errors.Is(err, directsdk.ErrLoggedOut) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []models.Built
	for _, m := range found {
		name := strings.TrimSuffix(m.ID, "[1m]")
		out = append(out, models.Built{
			ID:           name + "@claude-code",
			DisplayName:  m.Label + " (Claude Code)",
			Provider:     models.ProviderAnthropic,
			Source:       "claude code",
			ReleaseDate:  modelReleaseDate("", name),
			Service:      &ant.ClaudeCodeService{Client: claudeCodeClient, Model: m.ID},
			APIType:      models.APITypeAnthropicMessages,
			APIModelName: name,
		})
	}
	return out, nil
}
