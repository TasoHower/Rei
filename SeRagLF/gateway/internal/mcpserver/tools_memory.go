package mcpserver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"seraglf/internal/memory"
)

// -------------------- param structs --------------------

type MemoryDistillArgs struct {
	ConversationID string           `json:"conversation_id" jsonschema:"Stable session id shared across memory_context, memory_distill, memory_clear, and memory_save for one chat"`
	Messages       []memory.Message `json:"messages" jsonschema:"New turns since the last distill; must be non-empty. Include role and content for each message"`
}

type MemoryContextArgs struct {
	ConversationID string `json:"conversation_id" jsonschema:"Conversation whose distilled short-term snapshot should be read"`
}

type MemoryClearArgs struct {
	ConversationID string `json:"conversation_id" jsonschema:"Conversation whose Redis short-term buffer and distilled snapshot will be removed"`
}

type MemorySaveArgs struct {
	UserID         string `json:"user_id" jsonschema:"End-user identifier for long-term storage (facts in MySQL, summary vectors in Qdrant)"`
	ConversationID string `json:"conversation_id" jsonschema:"Conversation to persist; must have buffered messages (typically after one or more memory_distill calls)"`
}

type MemoryRecallArgs struct {
	UserID string `json:"user_id" jsonschema:"User whose long-term memories are queried"`
	Query  string `json:"query" jsonschema:"Natural-language query to rank relevant facts and past conversation summaries"`
	TopK   int    `json:"top_k" jsonschema:"Max items per channel (facts and summaries). Use 0 or omit for default 5"`
}

type MemoryUserProfileArgs struct {
	UserID string `json:"user_id" jsonschema:"User whose structured facts are listed or edited"`
	Action string `json:"action" jsonschema:"list (default) returns all facts; delete removes one fact when fact_id is set"`
	FactID int64  `json:"fact_id" jsonschema:"Required for action delete: database id of the fact row to remove"`
}

// -------------------- registration --------------------

func registerMemoryTools(server *mcp.Server, deps *Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_distill",
		Title:       "Distill short-term context",
		Description: "Append user or assistant turns to the session buffer and merge them into a rolling distilled context (topics, decisions, entities, action items, narrative).\n\nTypical flow: call after substantive new messages before memory_context or memory_save. Requires non-empty messages and a stable conversation_id.\n\nReturns the updated DistilledContext including version.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			OpenWorldHint:   ptrBool(false),
			DestructiveHint: ptrBool(false),
		},
	}, memoryDistillHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_context",
		Title:       "Read short-term context",
		Description: "Fetch the latest distilled snapshot for a conversation from Redis. Read-only.\n\nUse to inject current session memory into prompts. If nothing was distilled yet, response explains that explicitly.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			OpenWorldHint:   ptrBool(false),
			DestructiveHint: ptrBool(false),
			IdempotentHint:  true,
		},
	}, memoryContextHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_clear",
		Title:       "Clear short-term memory",
		Description: "Remove buffered messages and distilled short-term state for one conversation_id. Does not delete long-term facts or summaries.\n\nUse when the user starts a fresh topic in the same technical session or requests a reset.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			OpenWorldHint:   ptrBool(false),
			DestructiveHint: ptrBool(true),
		},
	}, memoryClearHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_save",
		Title:       "Persist to long-term memory",
		Description: "Flush the conversation from short-term buffers into durable storage: extract structured facts to MySQL and write an embedding summary to Qdrant.\n\nPrerequisites: same user_id and conversation_id as used in distill; conversation must contain messages. Often called at end of session or milestone.\n\nResponse may include facts_saved, summary_stored, or partial errors per subsystem.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			OpenWorldHint:   ptrBool(false),
			DestructiveHint: ptrBool(false),
		},
	}, memorySaveHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_recall",
		Title:       "Recall long-term memory",
		Description: "Semantic recall across two indexes: user facts and past conversation summaries, ranked by relevance to the query.\n\nUse at the beginning of a new session or when personalization requires historical user data. Read-only.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			OpenWorldHint:   ptrBool(false),
			DestructiveHint: ptrBool(false),
			IdempotentHint:  true,
		},
	}, memoryRecallHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_user_profile",
		Title:       "List or delete user facts",
		Description: "Inspect or curate structured facts for a user. action=list (or empty) returns all facts with ids for auditing. action=delete removes one fact by fact_id from long-term storage.\n\nPrefer memory_recall for open-ended semantic recall; use this for explicit profile maintenance.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			OpenWorldHint:   ptrBool(false),
			DestructiveHint: ptrBool(false),
		},
	}, memoryUserProfileHandler(deps))
}

// -------------------- handlers --------------------

func memoryDistillHandler(deps *Deps) mcp.ToolHandlerFor[MemoryDistillArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args MemoryDistillArgs) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		deps.Log.Info("tool called", "tool", "memory_distill", "conversation_id", args.ConversationID, "messages", len(args.Messages))

		if args.ConversationID == "" {
			return errResult("conversation_id is required"), nil, nil
		}
		if len(args.Messages) == 0 {
			return errResult("messages must not be empty"), nil, nil
		}

		if err := deps.ShortTerm.BufferMessages(ctx, args.ConversationID, args.Messages); err != nil {
			deps.Log.Error("buffer messages failed", "tool", "memory_distill", "error", err)
			return errResult(fmt.Sprintf("buffer messages failed: %v", err)), nil, nil
		}

		existing, err := deps.ShortTerm.Get(ctx, args.ConversationID)
		if err != nil {
			deps.Log.Error("read existing context failed", "tool", "memory_distill", "error", err)
			return errResult(fmt.Sprintf("read existing context failed: %v", err)), nil, nil
		}

		distilled, err := deps.Extractor.Distill(ctx, existing, args.Messages)
		if err != nil {
			deps.Log.Error("distill failed", "tool", "memory_distill", "error", err)
			return errResult(fmt.Sprintf("distill failed: %v", err)), nil, nil
		}
		distilled.ConversationID = args.ConversationID

		if err := deps.ShortTerm.Update(ctx, args.ConversationID, distilled); err != nil {
			deps.Log.Error("save context failed", "tool", "memory_distill", "error", err)
			return errResult(fmt.Sprintf("save context failed: %v", err)), nil, nil
		}

		deps.Log.Info("tool completed", "tool", "memory_distill", "conversation_id", args.ConversationID, "version", distilled.Version, "duration_ms", time.Since(start).Milliseconds())
		return jsonResult(map[string]any{
			"status":          "success",
			"conversation_id": args.ConversationID,
			"version":         distilled.Version,
			"context":         distilled,
		})
	}
}

func memoryContextHandler(deps *Deps) mcp.ToolHandlerFor[MemoryContextArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args MemoryContextArgs) (*mcp.CallToolResult, any, error) {
		if args.ConversationID == "" {
			return errResult("conversation_id is required"), nil, nil
		}
		dc, err := deps.ShortTerm.Get(ctx, args.ConversationID)
		if err != nil {
			return errResult(fmt.Sprintf("memory_context failed: %v", err)), nil, nil
		}
		if dc == nil {
			return jsonResult(map[string]any{
				"conversation_id": args.ConversationID,
				"context":         nil,
				"message":         "no context distilled yet for this conversation",
			})
		}
		return jsonResult(map[string]any{
			"conversation_id": args.ConversationID,
			"context":         dc,
		})
	}
}

func memoryClearHandler(deps *Deps) mcp.ToolHandlerFor[MemoryClearArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args MemoryClearArgs) (*mcp.CallToolResult, any, error) {
		if args.ConversationID == "" {
			return errResult("conversation_id is required"), nil, nil
		}
		if err := deps.ShortTerm.Clear(ctx, args.ConversationID); err != nil {
			return errResult(fmt.Sprintf("memory_clear failed: %v", err)), nil, nil
		}
		return jsonResult(map[string]any{
			"status":          "success",
			"conversation_id": args.ConversationID,
		})
	}
}

func memorySaveHandler(deps *Deps) mcp.ToolHandlerFor[MemorySaveArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args MemorySaveArgs) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		deps.Log.Info("tool called", "tool", "memory_save", "user_id", args.UserID, "conversation_id", args.ConversationID)

		if args.UserID == "" || args.ConversationID == "" {
			return errResult("user_id and conversation_id are required"), nil, nil
		}

		msgs, err := deps.ShortTerm.ListMessages(ctx, args.ConversationID)
		if err != nil {
			return errResult(fmt.Sprintf("read conversation failed: %v", err)), nil, nil
		}
		if len(msgs) == 0 {
			return errResult("conversation is empty, nothing to save"), nil, nil
		}

		var (
			facts   []memory.Fact
			summary string
			factErr error
			sumErr  error
			wg      sync.WaitGroup
		)

		wg.Add(2)
		go func() {
			defer wg.Done()
			facts, factErr = deps.Extractor.ExtractFacts(ctx, msgs)
		}()
		go func() {
			defer wg.Done()
			summary, sumErr = deps.Extractor.Summarize(ctx, msgs)
		}()
		wg.Wait()

		result := map[string]any{
			"user_id":         args.UserID,
			"conversation_id": args.ConversationID,
		}

		if factErr != nil {
			result["facts_error"] = factErr.Error()
		} else {
			affected, err := deps.LongTerm.UpsertFacts(ctx, args.UserID, args.ConversationID, facts)
			if err != nil {
				result["facts_error"] = err.Error()
			} else {
				result["facts_saved"] = affected
				result["facts_extracted"] = len(facts)
			}
		}

		if sumErr != nil {
			result["summary_error"] = sumErr.Error()
		} else {
			if err := deps.LongTerm.SaveSummary(ctx, args.UserID, args.ConversationID, summary); err != nil {
				result["summary_error"] = err.Error()
			} else {
				result["summary_stored"] = true
				result["summary_preview"] = truncateStr(summary, 200)
			}
		}

		deps.Log.Info("tool completed", "tool", "memory_save", "user_id", args.UserID, "conversation_id", args.ConversationID, "duration_ms", time.Since(start).Milliseconds())
		return jsonResult(result)
	}
}

func memoryRecallHandler(deps *Deps) mcp.ToolHandlerFor[MemoryRecallArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args MemoryRecallArgs) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		deps.Log.Info("tool called", "tool", "memory_recall", "user_id", args.UserID, "query_length", len(args.Query))

		if args.UserID == "" || args.Query == "" {
			return errResult("user_id and query are required"), nil, nil
		}
		topK := args.TopK
		if topK <= 0 {
			topK = 5
		}

		var (
			facts     []memory.Fact
			summaries []memory.ConversationSummary
			factErr   error
			sumErr    error
			wg        sync.WaitGroup
		)

		wg.Add(2)
		go func() {
			defer wg.Done()
			facts, factErr = deps.LongTerm.RecallFacts(ctx, args.UserID, args.Query, topK)
		}()
		go func() {
			defer wg.Done()
			summaries, sumErr = deps.LongTerm.RecallSummaries(ctx, args.UserID, args.Query, topK)
		}()
		wg.Wait()

		result := map[string]any{"user_id": args.UserID}

		if factErr != nil {
			result["facts_error"] = factErr.Error()
		} else {
			result["relevant_facts"] = facts
		}

		if sumErr != nil {
			result["conversations_error"] = sumErr.Error()
		} else {
			result["related_conversations"] = summaries
		}

		deps.Log.Info("tool completed", "tool", "memory_recall", "user_id", args.UserID, "duration_ms", time.Since(start).Milliseconds())
		return jsonResult(result)
	}
}

func memoryUserProfileHandler(deps *Deps) mcp.ToolHandlerFor[MemoryUserProfileArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args MemoryUserProfileArgs) (*mcp.CallToolResult, any, error) {
		if args.UserID == "" {
			return errResult("user_id is required"), nil, nil
		}

		switch args.Action {
		case "delete":
			if args.FactID <= 0 {
				return errResult("fact_id is required for delete action"), nil, nil
			}
			if err := deps.LongTerm.DeleteFact(ctx, args.FactID); err != nil {
				return errResult(fmt.Sprintf("delete fact failed: %v", err)), nil, nil
			}
			return jsonResult(map[string]any{"status": "deleted", "fact_id": args.FactID})

		case "list", "":
			facts, err := deps.LongTerm.GetFacts(ctx, args.UserID)
			if err != nil {
				return errResult(fmt.Sprintf("get facts failed: %v", err)), nil, nil
			}
			return jsonResult(map[string]any{"user_id": args.UserID, "facts": facts, "total": len(facts)})

		default:
			return errResult(fmt.Sprintf("unknown action %q, use 'list' or 'delete'", args.Action)), nil, nil
		}
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
