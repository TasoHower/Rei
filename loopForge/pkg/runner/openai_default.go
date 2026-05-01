package runner

import (
	"os"
	"strings"

	"github.com/TasoHower/Rei/loopForge/pkg/agent"
	openaiadapter "github.com/TasoHower/Rei/loopForge/pkg/model/adapters/openai"
)

func ApplyOpenAIFromConfig(a *agent.Agent, apiKey, baseURL, modelFallback string) {
	if a == nil || a.ChatModel != nil {
		return
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		key = firstNonEmptyStr(os.Getenv("OPENAI_API_KEY"))
	}
	if key == "" {
		return
	}
	base := strings.TrimSpace(baseURL)
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = firstNonEmptyStr(os.Getenv("OPENAI_BASE_URL"))
	}
	if base == "" {
		base = openaiadapter.DefaultBaseURL
	}
	name := strings.TrimSpace(a.ModelName)
	if name == "" {
		name = strings.TrimSpace(modelFallback)
	}
	if name == "" {
		name = firstNonEmptyStr(os.Getenv("OPENAI_MODEL"))
	}
	if name == "" {
		name = openaiadapter.DefaultModelName
	}
	a.ChatModel = openaiadapter.NewOpenAIChatModel(key, base, name)
}

func applyDefaultOpenAIIfNeeded(a *agent.Agent) {
	ApplyOpenAIFromConfig(a, "", "", "")
}
