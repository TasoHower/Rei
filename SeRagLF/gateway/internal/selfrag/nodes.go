package selfrag

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"seraglf/internal/embedding"
	"seraglf/internal/qdrant"
)

type Nodes struct {
	grader    model.BaseChatModel
	generator model.BaseChatModel
	embedder  embedding.Embedder
	vectorDB  qdrant.VectorStore
	log       *slog.Logger
}

func NewNodes(grader, generator model.BaseChatModel, embedder embedding.Embedder, vectorDB qdrant.VectorStore, log *slog.Logger) *Nodes {
	return &Nodes{grader: grader, generator: generator, embedder: embedder, vectorDB: vectorDB, log: log}
}

func (n *Nodes) Retrieve(ctx context.Context, s *State) (*State, error) {
	n.log.Debug("retrieve", "question_length", len(s.Question), "collection", s.Collection, "retry", s.Retries)

	vec, err := n.embedder.EmbedQuery(ctx, s.Question)
	if err != nil {
		n.log.Error("retrieve embed failed", "error", err)
		s.AddTrace("retrieve", fmt.Sprintf("embed error: %v", err))
		return s, nil
	}

	results, err := n.vectorDB.Search(ctx, s.Collection, vec, s.TopK)
	if err != nil {
		n.log.Error("retrieve search failed", "error", err)
		s.AddTrace("retrieve", fmt.Sprintf("search error: %v", err))
		return s, nil
	}

	s.Documents = make([]RetrievedDoc, 0, len(results))
	for _, r := range results {
		s.Documents = append(s.Documents, RetrievedDoc{
			Content: r.Content, Source: r.Source, RelevanceScore: float64(r.Score),
		})
	}
	n.log.Debug("retrieve done", "documents", len(s.Documents))
	s.AddTrace("retrieve", fmt.Sprintf("retrieved %d documents", len(s.Documents)))
	return s, nil
}

func (n *Nodes) GradeDocuments(ctx context.Context, s *State) (*State, error) {
	var relevant []RetrievedDoc
	for _, doc := range s.Documents {
		prompt := fmt.Sprintf(
			"You are a relevance grader. Given the question and document, respond with ONLY a JSON object {\"relevant\": true} or {\"relevant\": false}.\n\nQuestion: %s\n\nDocument: %s",
			s.Question, doc.Content,
		)
		resp, err := n.grader.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
		if err != nil {
			doc.RelevanceScore = 0.5
			relevant = append(relevant, doc)
			continue
		}
		text := resp.Content
		if strings.Contains(strings.ToLower(text), `"relevant": true`) || strings.Contains(strings.ToLower(text), `"relevant":true`) {
			doc.RelevanceScore = 1.0
			relevant = append(relevant, doc)
		}
	}
	s.Documents = relevant
	n.log.Debug("grade_documents done", "relevant", len(relevant))
	s.AddTrace("grade_documents", fmt.Sprintf("%d/%d relevant", len(relevant), len(s.Documents)))
	return s, nil
}

func (n *Nodes) Generate(ctx context.Context, s *State) (*State, error) {
	var ctxDocs strings.Builder
	for i, doc := range s.Documents {
		fmt.Fprintf(&ctxDocs, "[Doc %d] %s\n\n", i+1, doc.Content)
	}

	prompt := fmt.Sprintf(
		"Answer the question based ONLY on the provided context. If the context doesn't contain enough information, say so.\n\nContext:\n%s\nQuestion: %s",
		ctxDocs.String(), s.Question,
	)
	resp, err := n.generator.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		s.AddTrace("generate", fmt.Sprintf("error: %v", err))
		return s, nil
	}
	s.Generation = resp.Content
	s.AddTrace("generate", fmt.Sprintf("generated %d chars", len(s.Generation)))
	return s, nil
}

func (n *Nodes) CheckHallucination(ctx context.Context, s *State) (*State, error) {
	var docs strings.Builder
	for _, doc := range s.Documents {
		docs.WriteString(doc.Content)
		docs.WriteString("\n---\n")
	}

	prompt := fmt.Sprintf(
		"You are a hallucination grader. Determine if the answer is grounded in the source documents. Respond with ONLY {\"grounded\": true} or {\"grounded\": false}.\n\nDocuments:\n%s\nAnswer: %s",
		docs.String(), s.Generation,
	)
	resp, err := n.grader.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		s.Grounded = true
		s.AddTrace("check_hallucination", fmt.Sprintf("error: %v, assuming grounded", err))
		return s, nil
	}

	var result struct{ Grounded bool }
	if err := json.Unmarshal([]byte(ExtractJSON(resp.Content)), &result); err != nil {
		s.Grounded = !strings.Contains(strings.ToLower(resp.Content), "false")
	} else {
		s.Grounded = result.Grounded
	}
	n.log.Debug("check_hallucination done", "grounded", s.Grounded)
	s.AddTrace("check_hallucination", fmt.Sprintf("grounded=%v", s.Grounded))
	return s, nil
}

func (n *Nodes) GradeAnswer(ctx context.Context, s *State) (*State, error) {
	prompt := fmt.Sprintf(
		"You are an answer quality grader. Does the answer address the question? Respond with ONLY {\"useful\": true} or {\"useful\": false}.\n\nQuestion: %s\n\nAnswer: %s",
		s.Question, s.Generation,
	)
	resp, err := n.grader.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		s.AnswerUseful = true
		s.AddTrace("grade_answer", fmt.Sprintf("error: %v, assuming useful", err))
		return s, nil
	}

	var result struct{ Useful bool }
	if err := json.Unmarshal([]byte(ExtractJSON(resp.Content)), &result); err != nil {
		s.AnswerUseful = !strings.Contains(strings.ToLower(resp.Content), "false")
	} else {
		s.AnswerUseful = result.Useful
	}
	n.log.Debug("grade_answer done", "useful", s.AnswerUseful)
	s.AddTrace("grade_answer", fmt.Sprintf("useful=%v", s.AnswerUseful))
	return s, nil
}

func (n *Nodes) TransformQuery(ctx context.Context, s *State) (*State, error) {
	prompt := fmt.Sprintf(
		"You are a query rewriter. Rewrite the following question to improve retrieval results. Output ONLY the rewritten question, nothing else.\n\nOriginal question: %s",
		s.Question,
	)
	resp, err := n.grader.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		s.AddTrace("transform_query", fmt.Sprintf("rewrite error: %v", err))
	} else {
		s.Question = strings.TrimSpace(resp.Content)
		s.AddTrace("transform_query", fmt.Sprintf("rewritten to: %s", s.Question))
	}
	s.Retries++
	n.log.Info("query transformed", "retry", s.Retries, "new_question_length", len(s.Question))
	return s, nil
}

func ExtractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
