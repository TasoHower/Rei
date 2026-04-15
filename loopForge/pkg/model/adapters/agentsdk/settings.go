package agentsdk

import (
	lpmodel "loopforge/pkg/model"

	sdkmodel "github.com/agentizen/agent-sdk-go/pkg/model"
)

// CallConfigToSDKSettings maps loopForge CallConfig into agent-sdk-go Settings.
func CallConfigToSDKSettings(c lpmodel.CallConfig) *sdkmodel.Settings {
	s := &sdkmodel.Settings{}
	if c.Temperature != nil {
		v := *c.Temperature
		s.Temperature = &v
	}
	if c.TopP != nil {
		v := *c.TopP
		s.TopP = &v
	}
	if c.MaxTokens != nil {
		v := *c.MaxTokens
		s.MaxTokens = &v
	}
	if s.Temperature == nil && s.TopP == nil && s.MaxTokens == nil {
		return nil
	}
	return s
}
