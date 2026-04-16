// Lark (Ark) stream demo via pkg/model streaming Recv (volcengine-go-sdk).
//
// Env:
//   - LARK_API_KEY or DOUBAO_API_KEY or ARK_API_KEY (required)
//   - LARK_MODEL or ARK_MODEL or DOUBAO_MODEL (optional, default deepseek-v3-2-251201)
//   - LARK_BASE_URL or DOUBAO_BASE_URL (optional, default https://ark.cn-beijing.volces.com/api/v3)
//   - LARK_USER_MESSAGE or DOUBAO_USER_MESSAGE (optional, default asks 1+1)
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"loopforge/pkg/model"
	larkadapter "loopforge/pkg/model/adapters/lark"
)

const (
	defaultBaseURL  = "https://ark.cn-beijing.volces.com/api/v3"
	defaultModel    = "deepseek-v3-2-251201"
	defaultUserText = "1+1=?"
)

// streamConfig holds Ark settings for this binary.
type streamConfig struct {
	APIKey      string
	BaseURL     string
	Model       string
	UserMessage string
}

func loadStreamConfig() (streamConfig, error) {
	key := strings.TrimSpace(os.Getenv("LARK_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(os.Getenv("DOUBAO_API_KEY"))
	}
	if key == "" {
		key = strings.TrimSpace(os.Getenv("ARK_API_KEY"))
	}
	if key == "" {
		return streamConfig{}, fmt.Errorf("set LARK_API_KEY or DOUBAO_API_KEY or ARK_API_KEY")
	}
	base := strings.TrimSpace(os.Getenv("LARK_BASE_URL"))
	if base == "" {
		base = strings.TrimSpace(os.Getenv("DOUBAO_BASE_URL"))
	}
	if base == "" {
		base = defaultBaseURL
	}
	m := strings.TrimSpace(os.Getenv("LARK_MODEL"))
	if m == "" {
		m = strings.TrimSpace(os.Getenv("ARK_MODEL"))
	}
	if m == "" {
		m = strings.TrimSpace(os.Getenv("DOUBAO_MODEL"))
	}
	if m == "" {
		m = defaultModel
	}
	msg := strings.TrimSpace(os.Getenv("LARK_USER_MESSAGE"))
	if msg == "" {
		msg = strings.TrimSpace(os.Getenv("DOUBAO_USER_MESSAGE"))
	}
	if msg == "" {
		msg = defaultUserText
	}
	return streamConfig{
		APIKey:      key,
		BaseURL:     strings.TrimRight(base, "/"),
		Model:       m,
		UserMessage: msg,
	}, nil
}

// runLarkStream streams one user turn through the chat model and writes assistant tokens to w.
func runLarkStream(ctx context.Context, w io.Writer) error {
	if w == nil {
		w = io.Discard
	}
	cfg, err := loadStreamConfig()
	if err != nil {
		return err
	}

	cm := larkadapter.NewLarkChatModel(cfg.APIKey, cfg.BaseURL, cfg.Model)

	msgs := []*model.Message{
		{Role: model.RoleUser, Content: cfg.UserMessage},
	}

	fmt.Fprintf(w, "model=%s base=%s\n", cfg.Model, cfg.BaseURL)
	fmt.Fprintf(w, "user: %s\n", cfg.UserMessage)
	fmt.Fprintf(w, "assistant (stream): ")

	r, err := cm.Stream(ctx, msgs)
	if err != nil {
		return fmt.Errorf("Stream: %w", err)
	}

	for {
		m, err := r.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if m != nil {
			fmt.Fprint(w, m.Content)
		}
	}
	fmt.Fprintln(w)
	return nil
}
