package main

import (
	"fmt"
	"os"

	"github.com/bnulwh/ollama/runner"
)

func main() {
	// 调用 runner.Execute，传递命令行参数（排除程序名）
	if err := runner.Execute(os.Args[1:]); err != nil {
		// 错误处理：输出错误信息并退出程序
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
