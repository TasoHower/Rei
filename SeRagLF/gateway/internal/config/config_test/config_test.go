package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seraglf/internal/config"
)

func TestLoad_DefaultValues(t *testing.T) {
	cfg, err := config.Load("")
	require.NoError(t, err)

	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "text", cfg.Log.Format)
	assert.Equal(t, "stdio", cfg.Server.MCPTransport)
	assert.Equal(t, 8080, cfg.Server.HTTPPort)
	assert.Equal(t, "openai", cfg.LLM.Provider)
	assert.Equal(t, "gpt-4o-mini", cfg.LLM.GraderModel)
	assert.Equal(t, "gpt-4o", cfg.LLM.GeneratorModel)
	assert.Equal(t, 30, cfg.LLM.TimeoutSeconds)
	assert.Equal(t, "text-embedding-3-small", cfg.Embedding.Model)
	assert.Equal(t, "localhost:6334", cfg.Qdrant.Address)
	assert.Equal(t, 3, cfg.Defaults.MaxRetries)
	assert.Equal(t, 5, cfg.Defaults.TopK)
	assert.Equal(t, 512, cfg.Defaults.ChunkSize)
	assert.Equal(t, 64, cfg.Defaults.ChunkOverlap)
	assert.Equal(t, 1536, cfg.Defaults.EmbeddingDim)
	assert.Equal(t, "cosine", cfg.Defaults.Distance)
	assert.Equal(t, 24, cfg.Memory.ShortTermTTLHours)
	assert.Equal(t, 200, cfg.Memory.MaxConversationMessages)
	assert.Equal(t, "_memory", cfg.Memory.SummaryCollection)
	assert.Equal(t, "_user_facts", cfg.Memory.FactsCollection)
}

func TestLoad_YAMLFile(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
log:
  level: debug
  format: json
server:
  http_port: 9090
defaults:
  max_retries: 5
  top_k: 10
`
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yamlContent), 0644))

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, "json", cfg.Log.Format)
	assert.Equal(t, 9090, cfg.Server.HTTPPort)
	assert.Equal(t, 5, cfg.Defaults.MaxRetries)
	assert.Equal(t, 10, cfg.Defaults.TopK)
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("SERAGLF_LOG_LEVEL", "error")
	t.Setenv("SERAGLF_DEFAULTS_TOP_K", "20")

	cfg, err := config.Load("")
	require.NoError(t, err)

	assert.Equal(t, "error", cfg.Log.Level)
	assert.Equal(t, 20, cfg.Defaults.TopK)
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte(":::invalid:::yaml"), 0644))

	_, err := config.Load(path)
	assert.Error(t, err)
}

func TestLoad_PartialConfig(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
log:
  level: warn
`
	path := filepath.Join(dir, "partial.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yamlContent), 0644))

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, "warn", cfg.Log.Level)
	assert.Equal(t, "text", cfg.Log.Format, "unset fields should keep defaults")
	assert.Equal(t, 8080, cfg.Server.HTTPPort, "unset fields should keep defaults")
	assert.Equal(t, 3, cfg.Defaults.MaxRetries, "unset fields should keep defaults")
}
