package agent

import (
	"context"
	"strings"
	"testing"

	modeliface "loopforge/pkg/model/interface"
	"loopforge/pkg/model/types"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/exchange"
	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/request"

	"github.com/bytedance/sonic"
)

// seqSpawnThenFinalChatModel returns a spawn tool call on the first [Generate] round, then final text.
type seqSpawnThenFinalChatModel struct {
	step  int
	final string
}

func (m *seqSpawnThenFinalChatModel) Stream(ctx context.Context, input []*types.Message, opts ...types.CallOption) (types.MessageStreamReader, error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return types.NewSliceStreamReader([]*types.Message{msg}), nil
}

func (m *seqSpawnThenFinalChatModel) Generate(_ context.Context, _ []*types.Message, _ ...types.CallOption) (*types.Message, error) {
	m.step++
	if m.step == 1 {
		return &types.Message{
			Role:    types.RoleAssistant,
			Content: "",
			ToolCalls: []types.ToolCallPart{{
				ID:        "spawn1",
				Name:      "spawn_subagent",
				Arguments: `{"task": "sub","allow_child_spawn": false}`,
			}},
		}, nil
	}
	return &types.Message{Role: types.RoleAssistant, Content: m.final}, nil
}

func (m *seqSpawnThenFinalChatModel) WithTools(_ []*types.ToolInfo) (modeliface.ToolCallingChatModel, error) {
	return m, nil
}

var _ modeliface.ToolCallingChatModel = (*seqSpawnThenFinalChatModel)(nil)

func TestDefaultSpawner_RejectAtMaxDepth(t *testing.T) {
	t.Parallel()
	ch := newFinalTestModel("x")
	built := &Agent{Name: "c", ChatModel: ch}
	par := &Agent{
		SpawnEnabled:      true,
		ChildAgentBuilder: func(*exchange.SpawnSpec) *Agent { return built },
		ChatModel:         ch,
	}
	sp := NewDefaultSpawner(par, 2)
	res, err := sp.Spawn(context.Background(), exchange.RunRef{RunID: "p", Depth: 1},
		&exchange.SpawnSpec{Task: "t", Lifecycle: exchange.LifecycleEphemeral})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Status != exchange.SpawnRejected {
		t.Fatalf("got %+v, want rejected", res)
	}
}

func TestDefaultSpawner_ChildRunCompletes(t *testing.T) {
	t.Parallel()
	child := &Agent{
		Name:      "c",
		ChatModel: newFinalTestModel("from-child"),
	}
	par := &Agent{
		SpawnEnabled:      true,
		ChildAgentBuilder: func(*exchange.SpawnSpec) *Agent { return child.Clone() },
		ChatModel:         newFinalTestModel("unused"),
	}
	sp := NewDefaultSpawner(par, 2)
	res, err := sp.Spawn(context.Background(), exchange.RunRef{RunID: "root", Depth: 0},
		&exchange.SpawnSpec{Task: "do sub", Lifecycle: exchange.LifecycleEphemeral})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != exchange.SpawnCompleted {
		t.Fatalf("status %s", res.Status)
	}
	if res.FinalText != "from-child" {
		t.Fatalf("final %q", res.FinalText)
	}
}

func newFinalTestModel(text string) *mockTextOnlyModel {
	return &mockTextOnlyModel{text: text}
}

// mockTextOnlyModel returns one assistant text turn with no tools.
type mockTextOnlyModel struct{ text string }

func (m *mockTextOnlyModel) Stream(ctx context.Context, input []*types.Message, opts ...types.CallOption) (types.MessageStreamReader, error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return types.NewSliceStreamReader([]*types.Message{msg}), nil
}
func (m *mockTextOnlyModel) Generate(_ context.Context, _ []*types.Message, _ ...types.CallOption) (*types.Message, error) {
	return &types.Message{Role: types.RoleAssistant, Content: m.text}, nil
}
func (m *mockTextOnlyModel) WithTools(_ []*types.ToolInfo) (modeliface.ToolCallingChatModel, error) {
	return m, nil
}

var _ modeliface.ToolCallingChatModel = (*mockTextOnlyModel)(nil)

func TestBuildSpawnSubagent_NilWhenDisabled(t *testing.T) {
	t.Parallel()
	ref := &exchange.RunRef{RunID: "a", Depth: 0}
	req := &request.RuntimeRequest{}
	agent := &Agent{SpawnEnabled: false, ChildAgentBuilder: func(*exchange.SpawnSpec) *Agent { return &Agent{} }}
	if x := buildSpawnSubagentVarTool(ref, agent, req, nil, nil); x != nil {
		t.Fatalf("expected nil, got %v", x)
	}
}

func TestBuildSpawnSubagent_NilAtDepth(t *testing.T) {
	t.Parallel()
	ref := &exchange.RunRef{RunID: "a", Depth: 1, ParentRunID: "p"}
	req := &request.RuntimeRequest{}
	par := &Agent{
		SpawnEnabled:      true,
		ChildAgentBuilder: func(*exchange.SpawnSpec) *Agent { return &Agent{} },
		ChatModel:         newFinalTestModel("a"),
	}
	// default max depth 2: parent 1 + child would be 2, not < 2
	if x := buildSpawnSubagentVarTool(ref, par, req, nil, nil); x != nil {
		t.Fatalf("expected nil, got %v", x)
	}
}

func TestBuildSpawnSubagent_IncludeWhenAllowed(t *testing.T) {
	t.Parallel()
	ref := &exchange.RunRef{RunID: "a", Depth: 0}
	req := &request.RuntimeRequest{}
	par := &Agent{
		SpawnEnabled:      true,
		ChildAgentBuilder: func(*exchange.SpawnSpec) *Agent { return &Agent{ChatModel: newFinalTestModel("x")} },
		ChatModel:         newFinalTestModel("a"),
	}
	if x := buildSpawnSubagentVarTool(ref, par, req, nil, nil); x == nil {
		t.Fatal("expected non-nil")
	} else if x.Name != "spawn_subagent" {
		t.Fatalf("name %q", x.Name)
	}
}

// fixedMetricsSpawner returns a completed spawn result with known metrics (no real child model).
type fixedMetricsSpawner struct {
	m outcome.RunMetrics
}

func (f *fixedMetricsSpawner) Spawn(_ context.Context, _ exchange.RunRef, _ *exchange.SpawnSpec) (*exchange.SpawnResult, error) {
	return &exchange.SpawnResult{
		Status:    exchange.SpawnCompleted,
		FinalText: "synthetic",
		Metrics:   f.m,
	}, nil
}

func TestRunLoop_SpawnPath_RollupChildMetricsIntoParentOutcome(t *testing.T) {
	t.Parallel()
	seq := &seqSpawnThenFinalChatModel{final: "parent-final"}
	par := New(
		seq,
		WithName("parent"),
		WithSystemInstructions("p"),
		WithSpawn(func(*exchange.SpawnSpec) *Agent { return &Agent{} }),
	)
	par.Spawner = &fixedMetricsSpawner{m: outcome.RunMetrics{
		InputTokens: 100, OutputTokens: 50, TotalTokens: 150, Steps: 1,
	}}

	ch := make(chan *event.RuntimeEvent, 256)
	st := &LoopState{
		CurrentRunRef: &exchange.RunRef{RunID: "r", Depth: 0},
	}
	_ = par.RunLoop(context.Background(), &request.RuntimeRequest{UserMessage: "u", SessionID: "r"}, ch, nil, st)
	close(ch)
	var last *outcome.RuntimeOutcome
	for _, ev := range collectEventChannel(ch) {
		if qe := ev.QueryEnd(); qe != nil && qe.Outcome != nil {
			last = qe.Outcome
		}
	}
	if last == nil {
		t.Fatal("no QueryEnd outcome")
	}
	// Parent mock stream reports 0 tokens; only rollup from fixed spawner.
	if last.Metrics.InputTokens < 100 {
		t.Fatalf("want child input tokens rolled up, got %+v", last.Metrics)
	}
	if last.Metrics.OutputTokens < 50 {
		t.Fatalf("want child output tokens rolled up, got %+v", last.Metrics)
	}
	if last.Metrics.Steps < 1 {
		t.Fatalf("want child steps rolled up, got steps=%d", last.Metrics.Steps)
	}
}

func TestRunLoop_SpawnPath_ChildEventsForwardedToParentCh(t *testing.T) {
	t.Parallel()
	var childTmpl = &Agent{
		Name:      "child",
		ChatModel: newFinalTestModel("child-answer"),
	}
	seq := &seqSpawnThenFinalChatModel{final: "parent-final"}
	par := New(
		seq,
		WithName("parent"),
		WithSystemInstructions("p"),
		WithSpawn(func(spec *exchange.SpawnSpec) *Agent {
			c := childTmpl.Clone()
			c.SpawnEnabled = spec.AllowChildSpawn
			if spec.AllowChildSpawn {
				c.ChildAgentBuilder = func(*exchange.SpawnSpec) *Agent { return childTmpl.Clone() }
			}
			return c
		}),
	)

	ch := make(chan *event.RuntimeEvent, 256)
	st := &LoopState{
		CurrentRunRef: &exchange.RunRef{RunID: "r", Depth: 0},
	}
	ctx := context.Background()
	_ = par.RunLoop(ctx, &request.RuntimeRequest{UserMessage: "u", SessionID: "r"}, ch, nil, st)
	close(ch)
	events := collectEventChannel(ch)
	// In async mode, intermediate child LLM events are NOT forwarded through
	// the parent channel. Only parent events appear (2 rounds) + the final
	// child answer delivered by the async goroutine.
	var callLLM int
	for _, ev := range events {
		if ev.CallLLMStart() != nil {
			callLLM++
		}
	}
	if callLLM != 2 {
		t.Fatalf("expected 2 parent call_llm events, got %d", callLLM)
	}
	// Verify the child's final answer is forwarded as a single event
	var childAnswers int
	for _, ev := range events {
		if a := ev.Answer(); a != nil && strings.Contains(a.Delta, "child") {
			childAnswers++
		}
	}
	if childAnswers == 0 {
		t.Fatal("expected child final answer delivered to parent channel")
	}
}

// collectEventChannel returns events after a closed channel of pointers.
func collectEventChannel(ch <-chan *event.RuntimeEvent) (out []*event.RuntimeEvent) {
	for e := range ch {
		if e != nil {
			out = append(out, e)
		}
	}
	return out
}

func TestUnmarshal_SpawnResultRoundTrip(t *testing.T) {
	t.Parallel()
	orig := &exchange.SpawnResult{
		ChildRunRef: exchange.RunRef{RunID: "c1", ParentRunID: "p", Depth: 1},
		Status:      exchange.SpawnRejected,
		FinalText:   "",
		Error:       &exchange.SpawnError{Code: "x", Message: "y"},
	}
	s, err := sonic.MarshalString(orig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "rejected") {
		t.Fatalf("%s", s)
	}
}
