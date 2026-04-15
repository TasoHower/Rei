package ingest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Document struct {
	Content  string
	Metadata map[string]string
}

var supportedExts = map[string]bool{
	".txt": true, ".md": true, ".html": true, ".htm": true,
}

func ParseFile(path string) ([]Document, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return ParseDirectory(path)
	}

	ext := strings.ToLower(filepath.Ext(path))
	if !supportedExts[ext] {
		return nil, fmt.Errorf("unsupported format: %s", ext)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	content := string(data)
	if ext == ".html" || ext == ".htm" {
		content = stripHTMLTags(content)
	}

	return []Document{{
		Content:  content,
		Metadata: map[string]string{"source": path, "format": ext},
	}}, nil
}

func ParseDirectory(dir string) ([]Document, error) {
	var docs []Document
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !supportedExts[ext] {
			return nil
		}
		parsed, err := ParseFile(path)
		if err != nil {
			return nil
		}
		docs = append(docs, parsed...)
		return nil
	})
	return docs, err
}

func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}
