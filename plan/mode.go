package plan

import "github.com/tasohower/rei/agents"

// ReadOnlyOption 返回让运行进入只读模式的选项。
//
// Plan 模式的语义是「先只调研、不改动」：内核拒绝非只读工具（CanParallel=false），
// 因此不需要宿主自己维护白名单。
func ReadOnlyOption() agents.Option { return agents.WithReadOnlyMode() }
