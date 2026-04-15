package ingest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seraglf/internal/ingest"
)

func TestParseFile_TextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello text"), 0644))

	docs, err := ingest.ParseFile(path)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.Equal(t, "hello text", docs[0].Content)
	assert.Equal(t, path, docs[0].Metadata["source"])
	assert.Equal(t, ".txt", docs[0].Metadata["format"])
}

func TestParseFile_MarkdownFile(t *testing.T) {
	dir := t.TempDir()
	content := "# Title\n\nSome **bold** text."
	path := filepath.Join(dir, "doc.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	docs, err := ingest.ParseFile(path)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.Equal(t, content, docs[0].Content)
	assert.Equal(t, ".md", docs[0].Metadata["format"])
	assert.Equal(t, path, docs[0].Metadata["source"])
}

func TestParseFile_HTMLFile(t *testing.T) {
	dir := t.TempDir()
	html := "<html><body><p>Hello</p><div>World</div></body></html>"
	path := filepath.Join(dir, "page.html")
	require.NoError(t, os.WriteFile(path, []byte(html), 0644))

	docs, err := ingest.ParseFile(path)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.NotContains(t, docs[0].Content, "<p>")
	assert.NotContains(t, docs[0].Content, "<div>")
	assert.Contains(t, docs[0].Content, "Hello")
	assert.Contains(t, docs[0].Content, "World")
	assert.Equal(t, ".html", docs[0].Metadata["format"])
}

func TestParseFile_UnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	require.NoError(t, os.WriteFile(path, []byte("binary data"), 0644))

	docs, err := ingest.ParseFile(path)
	assert.Error(t, err)
	assert.Nil(t, docs)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestParseFile_NotFound(t *testing.T) {
	docs, err := ingest.ParseFile("/nonexistent/path/file.txt")
	assert.Error(t, err)
	assert.Nil(t, docs)
}

func TestParseDirectory(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("text A"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.md"), []byte("# B"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "c.pdf"), []byte("skip me"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "d.html"), []byte("<b>bold</b>"), 0644))

	docs, err := ingest.ParseFile(dir)
	require.NoError(t, err)
	assert.Len(t, docs, 3, "should parse .txt, .md, .html but skip .pdf")
}

func TestStripHTMLTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "nested tags",
			input: "<div><p>Hello <b>World</b></p></div>",
			want:  "Hello World",
		},
		{
			name:  "self-closing tags",
			input: "Line1<br/>Line2<hr/>End",
			want:  "Line1Line2End",
		},
		{
			name:  "no tags",
			input: "plain text without tags",
			want:  "plain text without tags",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "test.html")
			require.NoError(t, os.WriteFile(path, []byte(tt.input), 0644))

			docs, err := ingest.ParseFile(path)
			require.NoError(t, err)
			require.Len(t, docs, 1)
			assert.Equal(t, tt.want, docs[0].Content)
		})
	}
}
