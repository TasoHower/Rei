package runner

import (
	"os"
	"strings"

	"github.com/TasoHower/rei/loopForge/pkg/agent"
	deepseekadapter "github.com/TasoHower/rei/loopForge/pkg/model/adapters/deepseek"
)

const defaultDeepSeekModelName = "deepseek-chat"

func ApplyDeepSeekFromConfig(a *agent.Agent, apiKey, baseURL, modelFallback string) {
	if a == nil || a.ChatModel != nil {
		return
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		key = firstNonEmptyStr(os.Getenv("DEEPSEEK_API_KEY"))
	}
	if key == "" {
		return
	}
	base := strings.TrimSpace(baseURL)
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = firstNonEmptyStr(os.Getenv("DEEPSEEK_BASE_URL"))
	}
	if base == "" {
		base = deepseekadapter.DefaultBaseURL
	}
	name := strings.TrimSpace(a.ModelName)
	if name == "" {
		name = strings.TrimSpace(modelFallback)
	}
	if name == "" {
		name = firstNonEmptyStr(os.Getenv("DEEPSEEK_MODEL"))
	}
	if name == "" {
		name = defaultDeepSeekModelName
	}
	a.ChatModel = deepseekadapter.NewDeepSeekChatModel(key, base, name)
}
