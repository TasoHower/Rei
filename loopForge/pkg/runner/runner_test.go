package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/TasoHower/Rei/loopForge/pkg/agent"
	"github.com/TasoHower/Rei/loopForge/pkg/runtime/event"
	"github.com/TasoHower/Rei/loopForge/pkg/runtime/outcome"
	"github.com/TasoHower/Rei/loopForge/pkg/runtime/request"
)

func TestRunner_Transfer_ParallelToolCalls_AllSyntheticToolMessages(t *testing.T) {
	triage := agent.New(
		mockParallelToolTransferChatModel{target: "expert"},
		agent.WithName("triage"),
		agent.WithSystemInstructions("You are triage."),
	)
	expert := agent.New(
		mockFinalChatModel{text: "done with parallel tools."},
		agent.WithName("expert"),
		agent.WithSystemInstructions("You are an expert."),
	)
	triage.AddHandoff(expert)

	r := NewRunner(triage, WithMaxTransfers(5))
	ch := r.Run(context.Background(), &request.RuntimeRequest{
		SessionID:   "test-parallel-tc",
		UserMessage: "math",
	})
	events := collectEvents(ch)
	qe := findQueryEnd(events)
	if qe == nil || qe.Outcome == nil {
		t.Fatal("missing QueryEndPayload")
	}
	if qe.Outcome.Termination != outcome.TerminationCompleted {
		t.Fatalf("termination=%q", qe.Outcome.Termination)
	}
	if qe.Outcome.FinalText != "done with parallel tools." {
		t.Fatalf("FinalText=%q", qe.Outcome.FinalText)
	}
}

func TestRunner_Transfer_HappyPath(t *testing.T) {
	triage := agent.New(
		mockTransferChatModel{target: "expert"},
		agent.WithName("triage"),
		agent.WithDescription("Routes requests"),
		agent.WithSystemInstructions("You are triage."),
	)
	expert := agent.New(
		mockFinalChatModel{text: "The answer is 42."},
		agent.WithName("expert"),
		agent.WithDescription("Handles expert tasks"),
		agent.WithSystemInstructions("You are an expert."),
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
	// Triage: 1 LLM call (transfer tool). Expert: 1 LLM call (final text).
	if qe.Outcome.Metrics.Steps != 2 {
		t.Fatalf("Metrics.Steps=%d, want 2 (one LLM call per hop)", qe.Outcome.Metrics.Steps)
	}
	chain := qe.Outcome.TransferChain
	if len(chain) != 2 || chain[0] != "triage" || chain[1] != "expert" {
		t.Fatalf("TransferChain=%v, want [triage expert]", chain)
	}
}

func TestRunner_MaxTransfers_Exceeded(t *testing.T) {
	ping := agent.New(
		mockTransferChatModel{target: "pong"},
		agent.WithName("ping"),
	)
	pong := agent.New(
		mockTransferChatModel{target: "ping"},
		agent.WithName("pong"),
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
	solo := agent.New(
		mockFinalChatModel{text: "Just me."},
		agent.WithName("solo"),
		agent.WithSystemInstructions("solo agent"),
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
	if qe.Outcome.Metrics.Steps != 1 {
		t.Fatalf("Metrics.Steps=%d, want 1", qe.Outcome.Metrics.Steps)
	}
}

func TestRunner_Transfer_NoToolCallEvents(t *testing.T) {
	triage := agent.New(
		mockTransferChatModel{target: "expert"},
		agent.WithName("triage"),
	)
	expert := agent.New(
		mockFinalChatModel{text: "done"},
		agent.WithName("expert"),
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
	triage := agent.New(
		mockTransferChatModel{target: "expert"},
		agent.WithName("triage"),
	)
	expert := agent.New(
		mockFinalChatModel{text: "answer"},
		agent.WithName("expert"),
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

func TestRunner_WithSkillPath_InjectedIntoSystem(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(skillDir, "SKILL.md")
	content := "---\nname: demo\ndescription: Example skill for tests.\n---\n\nBody line.\n"
	if err := os.WriteFile(md, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cap := &captureSystemChatModel{}
	ag := agent.New(cap,
		agent.WithSystemInstructions("base"),
		agent.WithSkills(nil, "demo"),
		agent.WithMaxSteps(1),
	)
	r := NewRunner(ag, WithSkillPath(dir))
	ch := r.Run(context.Background(), &request.RuntimeRequest{
		SessionID:   "skill-path-test",
		UserMessage: "hi",
	})
	collectEvents(ch)
	sys := cap.LastSystem()
	if !strings.Contains(sys, "## Skill: demo") {
		t.Fatalf("missing skill section: %q", sys)
	}
	if !strings.Contains(sys, "Body line.") {
		t.Fatalf("missing body: %q", sys)
	}
}
