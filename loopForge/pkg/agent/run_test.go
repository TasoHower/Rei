package agent

import (
	"context"
	"sync"
	"testing"

	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/request"
)

func TestRunner_Transfer_HappyPath(t *testing.T) {
	triage := NewRunnerAgent(
		mockTransferChatModel{target: "expert"},
		WithName("triage"),
		WithDescription("Routes requests"),
		WithSystemInstructions("You are triage."),
	)
	expert := NewRunnerAgent(
		mockFinalChatModel{text: "The answer is 42."},
		WithName("expert"),
		WithDescription("Handles expert tasks"),
		WithSystemInstructions("You are an expert."),
	)
	triage.AddHandoff(expert)

	r := NewRunner(triage, WithMaxTransfers(5))

	ctx := context.Background()
	ch := r.Run(ctx, &request.RuntimeRequest{
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

func TestRunner_MaxTransfers_Exceeded(t *testing.T) {
	ping := NewRunnerAgent(
		mockTransferChatModel{target: "pong"},
		WithName("ping"),
	)
	pong := NewRunnerAgent(
		mockTransferChatModel{target: "ping"},
		WithName("pong"),
	)
	ping.AddHandoff(pong)
	pong.AddHandoff(ping)

	r := NewRunner(ping, WithMaxTransfers(3))

	ctx := context.Background()
	ch := r.Run(ctx, &request.RuntimeRequest{
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

func TestRunner_SingleAgent_NoTransfer(t *testing.T) {
	solo := NewRunnerAgent(
		mockFinalChatModel{text: "Just me."},
		WithName("solo"),
		WithSystemInstructions("solo agent"),
	)

	r := NewRunner(solo)

	ctx := context.Background()
	ch := r.Run(ctx, &request.RuntimeRequest{
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
}

func TestRunner_Transfer_NoToolCallEvents(t *testing.T) {
	triage := NewRunnerAgent(
		mockTransferChatModel{target: "expert"},
		WithName("triage"),
	)
	expert := NewRunnerAgent(
		mockFinalChatModel{text: "done"},
		WithName("expert"),
	)
	triage.AddHandoff(expert)

	r := NewRunner(triage)
	ch := r.Run(context.Background(), &request.RuntimeRequest{
		SessionID:   "test-no-toolcall",
		UserMessage: "test",
	})
	events := collectEvents(ch)

	for _, ev := range events {
		if ev.Type == event.EventToolCallStart || ev.Type == event.EventToolCallEnd {
			t.Fatalf("unexpected %s event during transfer", ev.Type)
		}
	}
}

func TestRunner_ConcurrentRuns(t *testing.T) {
	triage := NewRunnerAgent(
		mockTransferChatModel{target: "expert"},
		WithName("triage"),
	)
	expert := NewRunnerAgent(
		mockFinalChatModel{text: "answer"},
		WithName("expert"),
	)
	triage.AddHandoff(expert)

	r := NewRunner(triage, WithMaxTransfers(5))

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(id int) {
			defer wg.Done()
			ch := r.Run(context.Background(), &request.RuntimeRequest{
				SessionID:   "concurrent-" + string(rune('A'+id)),
				UserMessage: "test",
			})
			events := collectEvents(ch)
			qe := findQueryEnd(events)
			if qe == nil || qe.Outcome == nil {
				t.Errorf("segment %d: missing QueryEnd", id)
				return
			}
			if qe.Outcome.Termination != outcome.TerminationCompleted {
				t.Errorf("segment %d: termination=%q", id, qe.Outcome.Termination)
			}
		}(i)
	}
	wg.Wait()
}
