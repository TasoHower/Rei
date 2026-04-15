package selfrag_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seraglf/internal/selfrag"
)

func TestState_AddTrace(t *testing.T) {
	s := &selfrag.State{}

	s.AddTrace("retrieve", "retrieved 5 documents")
	s.AddTrace("grade_documents", "3/5 relevant")
	s.AddTrace("generate", "generated 120 chars")

	assert.Len(t, s.TraceSteps, 3)
	assert.Equal(t, "retrieve", s.TraceSteps[0].Node)
	assert.Equal(t, "retrieved 5 documents", s.TraceSteps[0].Detail)
	assert.Equal(t, "grade_documents", s.TraceSteps[1].Node)
	assert.Equal(t, "generate", s.TraceSteps[2].Node)
}
