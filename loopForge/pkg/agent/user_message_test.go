package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	modeliface "github.com/TasoHower/rei/loopForge/pkg/model/interface"
	"github.com/TasoHower/rei/loopForge/pkg/model/types"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/request"
	"github.com/TasoHower/rei/loopForge/pkg/variable"
)

func TestSystemPromptBuilderThenVarStoreBrace(t *testing.T) {
	vs := variable.New()
	vs.Set("name", "Ada")

	builder := func(pb SystemPromptBuildContext) (string, error) {
		if pb.BaseSystemPrompt != "sys {{name}}" {
			t.Fatalf("unexpected base system prompt: %q", pb.BaseSystemPrompt)
		}
		return pb.BaseSystemPrompt + "\n\n" + pb.VariablePromptBlock, nil
	}

	p0 := stringParamsFromVarStore(vs)
	base, err := builder(SystemPromptBuildContext{
		VarStore:            vs,
		Step:                0,
		BaseSystemPrompt:    "sys {{name}}",
		VariablePromptBlock: vs.PromptBlock(),
	})
	if err != nil {
		t.Fatal(err)
	}
	out := ReplaceDoubleBraceParams(base, p0)
	if out == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !strings.Contains(out, "sys Ada") {
		t.Fatalf("expected base system prompt replaced, got %q", out)
	}
	if !strings.Contains(out, "name = \"Ada\"") {
		t.Fatalf("got %q", out)
	}
}

type captureSystemPromptModel struct {
	mu         sync.Mutex
	lastSystem string
}

func (m *captureSystemPromptModel) Stream(_ context.Context, input []*types.Message, _ ...types.CallOption) (types.MessageStreamReader, error) {
	for _, msg := range input {
		if msg != nil && msg.Role == types.RoleSystem {
			m.mu.Lock()
			m.lastSystem = msg.Content
			m.mu.Unlock()
			break
		}
	}
	return types.NewSliceStreamReader([]*types.Message{
		{
			Role:    types.RoleAssistant,
			Content: "ok",
		},
	}), nil
}

func (m *captureSystemPromptModel) Generate(ctx context.Context, input []*types.Message, opts ...types.CallOption) (*types.Message, error) {
	_, err := m.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return &types.Message{
		Role:    types.RoleAssistant,
		Content: "ok",
	}, nil
}

func (m *captureSystemPromptModel) WithTools(_ []*types.ToolInfo) (modeliface.ToolCallingChatModel, error) {
	return m, nil
}

func (m *captureSystemPromptModel) LastSystem() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSystem
}

var _ modeliface.ToolCallingChatModel = (*captureSystemPromptModel)(nil)

func TestSystemPromptBuilder_AssignParamAndAppendVarBlock(t *testing.T) {
	mockModel := &captureSystemPromptModel{}
	a := New(mockModel,
		WithSystemInstructions("base {{topic}}"),
		WithVariable(),
		WithSystemPromptBuilder(func(pb SystemPromptBuildContext) (string, error) {
			pb.VarStore.Set("topic", "golang")
			return pb.BaseSystemPrompt + "\n\ncustom: {{topic}}", nil
		}),
		WithMaxSteps(1),
	)

	events := collectEvents(a.Run(context.Background(), &request.RuntimeRequest{
		SessionID:   "test-builder-assign",
		UserMessage: "hello",
	}))

	qe := findQueryEnd(events)
	if qe == nil || qe.Outcome == nil {
		t.Fatal("missing QueryEndPayload")
	}

	systemPrompt := mockModel.LastSystem()
	if !strings.Contains(systemPrompt, "base golang") {
		t.Fatalf("expected replaced base prompt, got %q", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "custom: golang") {
		t.Fatalf("expected replaced custom prompt, got %q", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "topic = \"golang\"") {
		t.Fatalf("expected variable prompt block appended, got %q", systemPrompt)
	}
}
