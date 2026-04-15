package selfrag

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/compose"
)

const (
	nodeRetrieve          = "retrieve"
	nodeGradeDocuments    = "grade_documents"
	nodeGenerate          = "generate"
	nodeCheckHallucinate  = "check_hallucination"
	nodeGradeAnswer       = "grade_answer"
	nodeTransformQuery    = "transform_query"
)

type Pipeline struct {
	runnable compose.Runnable[*State, *State]
}

func NewPipeline(ctx context.Context, nodes *Nodes) (*Pipeline, error) {
	graph := compose.NewGraph[*State, *State]()

	graph.AddLambdaNode(nodeRetrieve, compose.InvokableLambda(nodes.Retrieve))
	graph.AddLambdaNode(nodeGradeDocuments, compose.InvokableLambda(nodes.GradeDocuments))
	graph.AddLambdaNode(nodeGenerate, compose.InvokableLambda(nodes.Generate))
	graph.AddLambdaNode(nodeCheckHallucinate, compose.InvokableLambda(nodes.CheckHallucination))
	graph.AddLambdaNode(nodeGradeAnswer, compose.InvokableLambda(nodes.GradeAnswer))
	graph.AddLambdaNode(nodeTransformQuery, compose.InvokableLambda(nodes.TransformQuery))

	graph.AddEdge(compose.START, nodeRetrieve)
	graph.AddEdge(nodeRetrieve, nodeGradeDocuments)

	graph.AddBranch(nodeGradeDocuments, compose.NewGraphBranch(
		func(_ context.Context, s *State) (string, error) {
			if len(s.Documents) > 0 {
				return nodeGenerate, nil
			}
			if s.Retries >= s.MaxRetries {
				return nodeGenerate, nil
			}
			return nodeTransformQuery, nil
		},
		map[string]bool{nodeGenerate: true, nodeTransformQuery: true},
	))

	graph.AddEdge(nodeGenerate, nodeCheckHallucinate)

	graph.AddBranch(nodeCheckHallucinate, compose.NewGraphBranch(
		func(_ context.Context, s *State) (string, error) {
			if !s.Grounded && s.Retries < s.MaxRetries {
				return nodeGenerate, nil
			}
			return nodeGradeAnswer, nil
		},
		map[string]bool{nodeGradeAnswer: true, nodeGenerate: true},
	))

	graph.AddBranch(nodeGradeAnswer, compose.NewGraphBranch(
		func(_ context.Context, s *State) (string, error) {
			if s.AnswerUseful || s.Retries >= s.MaxRetries {
				return compose.END, nil
			}
			return nodeTransformQuery, nil
		},
		map[string]bool{compose.END: true, nodeTransformQuery: true},
	))

	graph.AddEdge(nodeTransformQuery, nodeRetrieve)

	runnable, err := graph.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("compile self-rag graph: %w", err)
	}
	return &Pipeline{runnable: runnable}, nil
}

func (p *Pipeline) Run(ctx context.Context, question, collection string, maxRetries, topK int) (*State, error) {
	state := &State{
		Question:   question,
		Collection: collection,
		MaxRetries: maxRetries,
		TopK:       topK,
	}
	return p.runnable.Invoke(ctx, state)
}
