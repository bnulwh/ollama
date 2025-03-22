package runner

import (
	"github.com/bnulwh/ollama/runner/llamarunner"  // 实现 llama.cpp 引擎的 Runner
	"github.com/bnulwh/ollama/runner/ollamarunner" // 实现 Ollama 原生引擎的 Runner
)

// 根据命令行参数选择并执行对应的运行器。
func Execute(args []string) error {
	// 如果第一个参数是 "runner"，则移除它。
	// 可能用于兼容旧版命令行格式（如 ollama runner --ollama-engine）
	if args[0] == "runner" {
		args = args[1:]
	}

	// 检查参数是否包含 --ollama-engine，若存在则移除该标志，并标记 newRunner 为 true。
	// newRunner 控制后续选择 ollamarunner（新引擎）或 llamarunner（默认引擎）。
	var newRunner bool
	if args[0] == "--ollama-engine" {
		args = args[1:]
		newRunner = true
	}

	if newRunner {
		return ollamarunner.Execute(args)
	} else {
		return llamarunner.Execute(args)
	}
}
