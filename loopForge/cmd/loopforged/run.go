package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

// Run runs the Lark (Ark) demo when LARK_API_KEY (or legacy DOUBAO_*/ARK_*) and model are set.
// Set LOOPFORGE_USE_MOCK=1 to run the offline mock instead.
// If credentials are missing and mock is not requested, prints setup hints to stderr.
func Run(ctx context.Context, w io.Writer) error {
	if w == nil {
		w = io.Discard
	}
	if UseMock() {
		return runMock(ctx, w)
	}
	cfg, ok := LoadLarkConfig()
	if !ok {
		printSetupHint(os.Stderr)
		return fmt.Errorf(
			"missing Lark credentials: set %s and %s (inference endpoint id, e.g. ep-...); optional %s; or set %s=1 for offline mock",
			EnvLarkAPIKey, EnvLarkModel, EnvLarkBaseURL, EnvUseMock,
		)
	}
	return runLarkRunner(ctx, w, cfg)
}

func printSetupHint(w io.Writer) {
	fmt.Fprintln(w, "loopForge Lark demo requires Volcengine Ark API access.")
	fmt.Fprintf(w, "  export %s=<your API key>   # or %s / %s\n", EnvLarkAPIKey, EnvDoubaoAPIKey, EnvArkAPIKey)
	fmt.Fprintf(w, "  export %s=<endpoint id from Ark console>   # or %s / %s\n", EnvLarkModel, EnvArkModel, EnvDoubaoModel)
	fmt.Fprintf(w, "  optional: export %s=<base url>  # default %s\n", EnvLarkBaseURL, DefaultLarkBaseURL)
	fmt.Fprintf(w, "  offline mock: export %s=1\n", EnvUseMock)
}
