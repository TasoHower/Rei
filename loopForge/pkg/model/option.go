package model

import "github.com/TasoHower/Rei/loopForge/pkg/model/types"

type CallConfig = types.CallConfig
type CallOption = types.CallOption

var (
	WithTemperature  = types.WithTemperature
	WithMaxTokens    = types.WithMaxTokens
	WithTopP         = types.WithTopP
	WithModel        = types.WithModel
	WithTools        = types.WithTools
	ApplyCallOptions = types.ApplyCallOptions
)
