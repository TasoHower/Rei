package runner

import (
	"os"
	"strings"

	"github.com/TasoHower/rei/loopForge/pkg/agent"
	larkadapter "github.com/TasoHower/rei/loopForge/pkg/model/adapters/lark"
)

const defaultLarkModelName = "deepseek-v3-2-251201"

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// ApplyLarkFromConfig sets Agent.ChatModel to Lark (Ark) when it is nil.
// Non-empty apiKey, baseURL override env; modelFallback fills the model id when Agent.ModelName is empty.
// If apiKey is empty, LARK_API_KEY / DOUBAO_API_KEY / ARK_API_KEY is used (same as Runner defaults).
func ApplyLarkFromConfig(a *agent.Agent, apiKey, baseURL, modelFallback string) {
	if a == nil || a.ChatModel != nil {
		return
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		key = firstNonEmptyStr(os.Getenv("LARK_API_KEY"), os.Getenv("DOUBAO_API_KEY"), os.Getenv("ARK_API_KEY"))
	}
	if key == "" {
		return
	}
	base := strings.TrimSpace(baseURL)
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = firstNonEmptyStr(os.Getenv("LARK_BASE_URL"), os.Getenv("DOUBAO_BASE_URL"))
	}
	if base == "" {
		base = larkadapter.DefaultBaseURL
	}
	name := strings.TrimSpace(a.ModelName)
	if name == "" {
		name = strings.TrimSpace(modelFallback)
	}
	if name == "" {
		name = firstNonEmptyStr(os.Getenv("LARK_MODEL"), os.Getenv("ARK_MODEL"), os.Getenv("DOUBAO_MODEL"))
	}
	if name == "" {
		name = defaultLarkModelName
	}
	a.ChatModel = larkadapter.NewLarkChatModel(key, base, name)
}

func applyDefaultLarkIfNeeded(a *agent.Agent) {
	ApplyLarkFromConfig(a, "", "", "")
}
