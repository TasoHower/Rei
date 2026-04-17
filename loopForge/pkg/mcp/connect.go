package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"loopforge/pkg/mcp/cfg"
)

// ConnectClientSession dials an MCP server per profile and returns a [sdkmcp.ClientSession].
// The stop function closes the session (and tears down stdio subprocess when applicable).
// Shared by debug and future BootstrapToolInfos.
func ConnectClientSession(ctx context.Context, profile cfg.MCPServerProfile) (*sdkmcp.ClientSession, func(), error) {
	transport, err := transportForProfile(ctx, profile)
	if err != nil {
		return nil, nil, err
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "loopforge", Version: "0.6.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, nil, err
	}
	stop := func() { _ = session.Close() }
	return session, stop, nil
}

func transportForProfile(ctx context.Context, profile cfg.MCPServerProfile) (sdkmcp.Transport, error) {
	switch profile.Transport {
	case cfg.MCPTransportStdio:
		return stdioTransport(ctx, profile)
	case cfg.MCPTransportStreamableHTTP:
		return streamableHTTPTransport(profile)
	default:
		return nil, fmt.Errorf("mcp: unsupported transport %q", profile.Transport)
	}
}

func stdioTransport(ctx context.Context, profile cfg.MCPServerProfile) (*sdkmcp.CommandTransport, error) {
	if len(profile.Command) == 0 {
		return nil, fmt.Errorf("mcp stdio: empty Command")
	}
	var cmd *exec.Cmd
	if len(profile.Command) == 1 {
		cmd = exec.CommandContext(ctx, profile.Command[0])
	} else {
		cmd = exec.CommandContext(ctx, profile.Command[0], profile.Command[1:]...)
	}
	if len(profile.Env) > 0 {
		cmd.Env = envWithOverride(os.Environ(), profile.Env)
	}
	return &sdkmcp.CommandTransport{Command: cmd}, nil
}

func streamableHTTPTransport(profile cfg.MCPServerProfile) (*sdkmcp.StreamableClientTransport, error) {
	endpoint := strings.TrimSpace(profile.URL)
	if endpoint == "" {
		return nil, fmt.Errorf("mcp streamable_http: empty URL")
	}
	t := &sdkmcp.StreamableClientTransport{Endpoint: endpoint}
	if len(profile.Headers) > 0 {
		h := make(http.Header)
		for k, v := range profile.Headers {
			h.Set(k, v)
		}
		base := http.DefaultTransport
		if t.HTTPClient != nil && t.HTTPClient.Transport != nil {
			base = t.HTTPClient.Transport
		}
		client := &http.Client{}
		if t.HTTPClient != nil {
			*client = *t.HTTPClient
		}
		client.Transport = &headerRoundTripper{base: base, extra: h}
		t.HTTPClient = client
	}
	return t, nil
}

type headerRoundTripper struct {
	base  http.RoundTripper
	extra http.Header
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, vals := range h.extra {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	return h.base.RoundTrip(req)
}

func envWithOverride(base []string, override map[string]string) []string {
	m := make(map[string]string)
	for _, e := range base {
		if i := strings.IndexByte(e, '='); i > 0 {
			m[e[:i]] = e[i+1:]
		}
	}
	for k, v := range override {
		m[k] = v
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}
