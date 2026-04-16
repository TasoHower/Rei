package transfer

import (
	"context"
	"testing"

	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/request"
)

func TestOrchestrator_Transfer_HappyPath(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(AgentConfig{
		Name:               "triage",
		Description:        "Routes requests",
		ChatModel:          mockTransferChatModel{target: "expert"},
		SystemInstructions: "You are triage.",
	})
	_ = registry.Register(AgentConfig{
		Name:               "expert",
		Description:        "Handles expert tasks",
		ChatModel:          mockFinalChatModel{text: "The answer is 42."},
		SystemInstructions: "You are an expert.",
	})

	orch := NewOrchestrator(registry, "triage", WithMaxTransfers(5))

	ctx := context.Background()
	ch := orch.Run(ctx, &request.RuntimeRequest{
		SessionID:   "test-transfer",
		UserMessage: "Help me with math",
	})

	events := collectEvents(ch)

	var transferEv *event.AgentTransferPayload
	for _, ev := range events {
		if at := ev.AgentTransfer(); at != nil {
			transferEv = at
			break
		}
	}
	if transferEv == nil {
		t.Fatal("expected agent_transfer event")
	}
	if transferEv.FromAgent != "triage" {
		t.Fatalf("FromAgent=%q, want triage", transferEv.FromAgent)
	}
	if transferEv.ToAgent != "expert" {
		t.Fatalf("ToAgent=%q, want expert", transferEv.ToAgent)
	}
	if transferEv.Reason != "user needs math help" {
		t.Fatalf("Reason=%q", transferEv.Reason)
	}

	qe := findQueryEnd(events)
	if qe == nil || qe.Outcome == nil {
		t.Fatal("missing QueryEndPayload")
	}
	if qe.Outcome.Termination != outcome.TerminationCompleted {
		t.Fatalf("termination=%q, want completed", qe.Outcome.Termination)
	}
	if qe.Outcome.FinalText != "The answer is 42." {
		t.Fatalf("FinalText=%q", qe.Outcome.FinalText)
	}
	chain := qe.Outcome.TransferChain
	if len(chain) != 2 || chain[0] != "triage" || chain[1] != "expert" {
		t.Fatalf("TransferChain=%v, want [triage expert]", chain)
	}
}

func TestOrchestrator_MaxTransfers_Exceeded(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(AgentConfig{
		Name:      "ping",
		ChatModel: mockTransferChatModel{target: "pong"},
	})
	_ = registry.Register(AgentConfig{
		Name:      "pong",
		ChatModel: mockTransferChatModel{target: "ping"},
	})

	orch := NewOrchestrator(registry, "ping", WithMaxTransfers(3))

	ctx := context.Background()
	ch := orch.Run(ctx, &request.RuntimeRequest{
		SessionID:   "test-loop",
		UserMessage: "loop forever",
	})

	events := collectEvents(ch)

	var sawError bool
	for _, ev := range events {
		if ep := ev.Error(); ep != nil && ep.Code == "max_transfers" {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("expected max_transfers error")
	}

	transferCount := 0
	for _, ev := range events {
		if ev.Type == event.EventAgentTransfer {
			transferCount++
		}
	}
	if transferCount != 3 {
		t.Fatalf("transferCount=%d, want 3", transferCount)
	}
}

func TestOrchestrator_SingleAgent_NoTransfer(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(AgentConfig{
		Name:               "solo",
		ChatModel:          mockFinalChatModel{text: "Just me."},
		SystemInstructions: "solo agent",
	})

	orch := NewOrchestrator(registry, "solo")

	ctx := context.Background()
	ch := orch.Run(ctx, &request.RuntimeRequest{
		SessionID:   "test-solo",
		UserMessage: "hello",
	})
	events := collectEvents(ch)

	qe := findQueryEnd(events)
	if qe == nil || qe.Outcome == nil {
		t.Fatal("missing QueryEndPayload")
	}
	if qe.Outcome.Termination != outcome.TerminationCompleted {
		t.Fatalf("termination=%q", qe.Outcome.Termination)
	}
	if qe.Outcome.FinalText != "Just me." {
		t.Fatalf("FinalText=%q", qe.Outcome.FinalText)
	}
	if len(qe.Outcome.TransferChain) != 1 || qe.Outcome.TransferChain[0] != "solo" {
		t.Fatalf("TransferChain=%v", qe.Outcome.TransferChain)
	}
}
