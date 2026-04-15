package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Log       LogConfig       `mapstructure:"log"`
	Server    ServerConfig    `mapstructure:"server"`
	LLM       LLMConfig       `mapstructure:"llm"`
	Embedding EmbeddingConfig `mapstructure:"embedding"`
	Qdrant    QdrantConfig    `mapstructure:"qdrant"`
	MySQL     MySQLConfig     `mapstructure:"mysql"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Memory    MemoryConfig    `mapstructure:"memory"`
	Defaults  DefaultsConfig  `mapstructure:"defaults"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type ServerConfig struct {
	MCPTransport string `mapstructure:"mcp_transport"` // stdio | http
	HTTPPort     int    `mapstructure:"http_port"`
}

type LLMConfig struct {
	Provider       string `mapstructure:"provider"` // openai | ark
	APIKey         string `mapstructure:"api_key"`
	BaseURL        string `mapstructure:"base_url"`
	GraderModel    string `mapstructure:"grader_model"`
	GeneratorModel string `mapstructure:"generator_model"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
}

type EmbeddingConfig struct {
	Provider string `mapstructure:"provider"` // openai | ark
	APIKey   string `mapstructure:"api_key"`
	BaseURL  string `mapstructure:"base_url"`
	Model    string `mapstructure:"model"`
}

type QdrantConfig struct {
	Address        string `mapstructure:"address"`
	APIKey         string `mapstructure:"api_key"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
}

type MySQLConfig struct {
	DSN string `mapstructure:"dsn"`
}

type RedisConfig struct {
	Address       string `mapstructure:"address"`
	Password      string `mapstructure:"password"`
	DB            int    `mapstructure:"db"`
	CacheTTLHours int    `mapstructure:"cache_ttl_hours"`
}

type MemoryConfig struct {
	ShortTermTTLHours      int    `mapstructure:"short_term_ttl_hours"`
	MaxConversationMessages int    `mapstructure:"max_conversation_messages"`
	SummaryCollection      string `mapstructure:"summary_collection"`
	FactsCollection        string `mapstructure:"facts_collection"`
}

type DefaultsConfig struct {
	MaxRetries   int    `mapstructure:"max_retries"`
	TopK         int    `mapstructure:"top_k"`
	ChunkSize    int    `mapstructure:"chunk_size"`
	ChunkOverlap int    `mapstructure:"chunk_overlap"`
	EmbeddingDim int    `mapstructure:"embedding_dim"`
	Distance     string `mapstructure:"distance"`
}

func Load(path string) (*Config, error) {
	v := viper.New()

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "text")
	v.SetDefault("server.mcp_transport", "stdio")
	v.SetDefault("server.http_port", 8080)

	v.SetDefault("llm.provider", "openai")
	v.SetDefault("llm.grader_model", "gpt-4o-mini")
	v.SetDefault("llm.generator_model", "gpt-4o")
	v.SetDefault("llm.timeout_seconds", 30)

	v.SetDefault("embedding.provider", "openai")
	v.SetDefault("embedding.model", "text-embedding-3-small")

	v.SetDefault("qdrant.address", "localhost:6334")
	v.SetDefault("qdrant.timeout_seconds", 5)
	v.SetDefault("mysql.dsn", "root:localtest@tcp(localhost:3306)/seraglf?charset=utf8mb4&parseTime=True&loc=Local")
	v.SetDefault("redis.address", "localhost:6379")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.cache_ttl_hours", 24)
	v.SetDefault("memory.short_term_ttl_hours", 24)
	v.SetDefault("memory.max_conversation_messages", 200)
	v.SetDefault("memory.summary_collection", "_memory")
	v.SetDefault("memory.facts_collection", "_user_facts")
	v.SetDefault("defaults.max_retries", 3)
	v.SetDefault("defaults.top_k", 5)
	v.SetDefault("defaults.chunk_size", 512)
	v.SetDefault("defaults.chunk_overlap", 64)
	v.SetDefault("defaults.embedding_dim", 1536)
	v.SetDefault("defaults.distance", "cosine")

	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
	}

	v.SetEnvPrefix("SERAGLF")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "warning: no config file found, using defaults and env vars\n")
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return cfg, nil
}
