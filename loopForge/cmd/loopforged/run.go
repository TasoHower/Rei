package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

// Run runs the Doubao (Ark) demo when DOUBAO_API_KEY and DOUBAO_MODEL are set.
// Set LOOPFORGE_USE_MOCK=1 to run the offline mock instead.
// If credentials are missing and mock is not requested, prints setup hints to stderr.
func Run(ctx context.Context, w io.Writer) error {
	if w == nil {
		w = io.Discard
	}
	if UseMock() {
		return runMock(ctx, w)
	}
	cfg, ok := LoadDoubaoConfig()
	if !ok {
		printSetupHint(os.Stderr)
		return fmt.Errorf(
			"missing Doubao credentials: set %s and %s (inference endpoint id, e.g. ep-...); optional %s; or set %s=1 for offline mock",
			EnvDoubaoAPIKey, EnvDoubaoModel, EnvDoubaoBaseURL, EnvUseMock,
		)
	}
	return runDoubaoRunner(ctx, w, cfg)
}

func printSetupHint(w io.Writer) {
	fmt.Fprintln(w, "loopForge Doubao demo requires Volcengine Ark API access.")
	fmt.Fprintf(w, "  export %s=<your API key>   # or %s\n", EnvDoubaoAPIKey, EnvArkAPIKey)
	fmt.Fprintf(w, "  export %s=<endpoint id from Ark console>\n", EnvDoubaoModel)
	fmt.Fprintf(w, "  optional: export %s=<base url>  # default %s\n", EnvDoubaoBaseURL, DefaultDoubaoBaseURL)
	fmt.Fprintf(w, "  offline mock: export %s=1\n", EnvUseMock)
}
