package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	if err := runLarkStream(context.Background(), os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "larkdemo: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nEnv:\n")
		fmt.Fprintf(os.Stderr, "  LARK_API_KEY or DOUBAO_API_KEY or ARK_API_KEY   required\n")
		fmt.Fprintf(os.Stderr, "  LARK_MODEL or ARK_MODEL or DOUBAO_MODEL         default deepseek-v3-2-251201\n")
		fmt.Fprintf(os.Stderr, "  LARK_BASE_URL or DOUBAO_BASE_URL                default https://ark.cn-beijing.volces.com/api/v3\n")
		fmt.Fprintf(os.Stderr, "  LARK_USER_MESSAGE or DOUBAO_USER_MESSAGE        default 1+1=?\n")
		os.Exit(1)
	}
}
