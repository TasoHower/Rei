package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/redis/go-redis/v9"

	"seraglf/internal/config"
	"seraglf/internal/embedding"
	"seraglf/internal/llm"
	"seraglf/internal/logger"
	"seraglf/internal/mcpserver"
	"seraglf/internal/memory"
	"seraglf/internal/qdrant"
	"seraglf/internal/selfrag"
	"seraglf/internal/store"
)

func main() {
	cfgPath := flag.String("config", "", "config file path")
	transport := flag.String("transport", "", "override transport: stdio or http")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: load config: %v\n", err)
		os.Exit(1)
	}

	log := logger.Setup(logger.Config{Level: cfg.Log.Level, Format: cfg.Log.Format})

	if *transport != "" {
		cfg.Server.MCPTransport = *transport
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	llmProvider, err := llm.NewProvider(ctx, cfg.LLM)
	if err != nil {
		log.Error("init llm failed", "error", err)
		os.Exit(1)
	}
	log.Info("llm provider ready", "grader", cfg.LLM.GraderModel, "generator", cfg.LLM.GeneratorModel)

	embedder, err := embedding.NewService(ctx, cfg.Embedding)
	if err != nil {
		log.Error("init embedding failed", "error", err)
		os.Exit(1)
	}
	log.Info("embedding service ready", "model", cfg.Embedding.Model)

	vectorDB, err := qdrant.NewClient(cfg.Qdrant.Address, cfg.Qdrant.APIKey)
	if err != nil {
		log.Error("init qdrant failed", "error", err)
		os.Exit(1)
	}
	defer vectorDB.Close()
	log.Info("qdrant client ready", "address", cfg.Qdrant.Address)

	nodes := selfrag.NewNodes(llmProvider.Grader, llmProvider.Generator, embedder, vectorDB, log.With("module", "selfrag"))
	pipeline, err := selfrag.NewPipeline(ctx, nodes)
	if err != nil {
		log.Error("init self-rag pipeline failed", "error", err)
		os.Exit(1)
	}
	log.Info("self-rag pipeline compiled")

	mysqlDB, err := store.NewMySQL(cfg.MySQL.DSN)
	if err != nil {
		log.Error("init mysql failed", "error", err)
		os.Exit(1)
	}
	defer mysqlDB.Close()
	log.Info("mysql ready")

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Error("ping redis failed", "error", err)
		os.Exit(1)
	}
	log.Info("redis ready", "address", cfg.Redis.Address)

	shortTerm := memory.NewShortTermStore(rdb, cfg.Memory, log.With("module", "short_term"))
	longTerm := memory.NewLongTermStore(mysqlDB, vectorDB, embedder, cfg.Memory, log.With("module", "long_term"))
	extractor := memory.NewExtractor(llmProvider.Grader, log.With("module", "extractor"))

	if err := longTerm.EnsureCollections(ctx, uint64(cfg.Defaults.EmbeddingDim)); err != nil {
		log.Warn("ensure memory collections failed", "error", err)
	}

	server := mcpserver.New(&mcpserver.Deps{
		Cfg:       cfg,
		Pipeline:  pipeline,
		Embedder:  embedder,
		VectorDB:  vectorDB,
		ShortTerm: shortTerm,
		LongTerm:  longTerm,
		Extractor: extractor,
		Log:       log.With("module", "mcp"),
	})

	switch cfg.Server.MCPTransport {
	case "stdio":
		log.Info("starting MCP server", "transport", "stdio")
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			log.Error("mcp stdio failed", "error", err)
			os.Exit(1)
		}

	case "http", "streamable_http":
		addr := fmt.Sprintf(":%d", cfg.Server.HTTPPort)
		handler := mcp.NewStreamableHTTPHandler(
			func(r *http.Request) *mcp.Server { return server },
			nil,
		)
		mux := http.NewServeMux()
		mux.Handle("/mcp", handler)
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"ok"}`))
		})
		log.Info("starting MCP server", "transport", "streamable_http", "address", addr)
		srv := &http.Server{Addr: addr, Handler: mux}
		go func() {
			<-ctx.Done()
			srv.Close()
		}()
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("mcp http failed", "error", err)
			os.Exit(1)
		}

	default:
		log.Error("unknown transport", "transport", cfg.Server.MCPTransport)
		os.Exit(1)
	}
}
