package agent

import (
	"testing"

	"loopforge/pkg/variable"
)

func TestReplaceDoubleBraceParams(t *testing.T) {
	params := map[string]string{
		"username": "Ada",
		"user_id":  "42",
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "case_insensitive",
			in:   "Hello {{UserName}}",
			want: "Hello Ada",
		},
		{
			name: "trim_inner_spaces",
			in:   "Hi {{  UserName  }}",
			want: "Hi Ada",
		},
		{
			name: "user_name_not_username",
			in:   "{{UserName}} vs {{user_name}}",
			want: "Ada vs {{user_name}}",
		},
		{
			name: "unknown_kept",
			in:   "x={{foo}}",
			want: "x={{foo}}",
		},
		{
			name: "empty_params_noop",
			in:   "{{a}}",
			want: "{{a}}",
		},
		{
			name: "empty_inner_kept",
			in:   "a{{  }}b",
			want: "a{{  }}b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p map[string]string
			if tt.name != "empty_params_noop" {
				p = params
			}
			got := ReplaceDoubleBraceParams(tt.in, p)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestReplaceDoubleBraceParams_unclosed(t *testing.T) {
	params := map[string]string{"a": "1"}
	in := "pre {{a}} mid {{x"
	got := ReplaceDoubleBraceParams(in, params)
	want := "pre 1 mid {{x"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStringParamsFromVarStore(t *testing.T) {
	s := variable.New()
	s.Set("UserName", "bob")
	s.Set("empty", nil)
	p := stringParamsFromVarStore(s)
	if p == nil {
		t.Fatal("expected non-nil map")
	}
	out := ReplaceDoubleBraceParams("{{username}} {{empty}}", p)
	if out != `bob <unset>` {
		t.Fatalf("got %q", out)
	}
}
