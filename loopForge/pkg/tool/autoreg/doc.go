// Package autoreg provides reflection-based auto-generation of JSON Schema
// Parameters for ToolInfo, eliminating manual map[string]interface{} definitions.
//
// Usage (recommended):
//
//	type MyToolParams struct {
//	    Query string `json:"query" description:"Search query"`
//	    Limit int    `json:"limit,omitempty" description:"Max results"`
//	}
//
//	tool := autoreg.NewToolFromStruct("search", "Performs a search",
//	    func(ctx context.Context, p MyToolParams) (string, error) {
//	        return doSearch(ctx, p.Query, p.Limit)
//	    },
//	)
//
// Migration (deprecated — custom Parameters with warning):
//
//	tool := autoreg.NewToolFromStruct("search", "Performs a search",
//	    func(ctx context.Context, p MyToolParams) (string, error) {
//	        return doSearch(ctx, p.Query, p.Limit)
//	    },
//	    autoreg.WithParameters(legacyParams),
//	)
package autoreg
