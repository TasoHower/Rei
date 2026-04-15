package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	if err := runDoubaoStream(context.Background(), os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "doubaodemo: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nEnv:\n")
		fmt.Fprintf(os.Stderr, "  ARK_API_KEY or DOUBAO_API_KEY   required\n")
		fmt.Fprintf(os.Stderr, "  ARK_MODEL or DOUBAO_MODEL       default deepseek-v3-2-251201\n")
		fmt.Fprintf(os.Stderr, "  DOUBAO_BASE_URL                 default https://ark.cn-beijing.volces.com/api/v3\n")
		fmt.Fprintf(os.Stderr, "  DOUBAO_USER_MESSAGE             default 1+1=?\n")
		os.Exit(1)
	}
}
