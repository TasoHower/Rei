package main

import (
	"context"
	"fmt"
	"os"

	"loopforge/internal/defaults"
)

func main() {
	fmt.Fprintf(os.Stdout, "%s %s — agent-sdk-go runner demo\n\n", defaults.ServiceName, defaults.ServiceVersion)
	if err := Run(context.Background(), os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "demo failed: %v\n", err)
		os.Exit(1)
	}
}
