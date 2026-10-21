package builtin

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// skippedDirs 是 glob / grep 永不扫描的目录：要么是生成物，要么体量过大，
// 要么是版本控制内部结构。
var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "vendor": true,
}

// doublestarGlob 展开支持 `**`（任意深度）的通配符，同时保留标准 `*`、`?` 与字符类。
func doublestarGlob(pattern string) ([]string, error) {
	pattern = filepath.ToSlash(pattern)

	// 从最长的「无通配符目录前缀」开始遍历，避免从根目录全盘扫描。
	base := pattern
	if idx := strings.IndexAny(pattern, "*?["); idx >= 0 {
		base = pattern[:idx]
		if cut := strings.LastIndex(base, "/"); cut >= 0 {
			base = base[:cut]
		} else {
			base = "."
		}
	}
	if base == "" {
		base = "/"
	}
	if _, err := os.Stat(base); err != nil {
		return nil, err
	}

	re, err := compileGlob(pattern)
	if err != nil {
		return nil, err
	}

	var out []string
	err = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 读不了的子树直接跳过，而不是让整个工具失败
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] && path != base {
				return fs.SkipDir
			}
			return nil
		}
		if re.MatchString(filepath.ToSlash(path)) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func compileGlob(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			end := strings.IndexByte(pattern[i:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			b.WriteString(pattern[i : i+end+1])
			i += end
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// grepFiles 在 root 下搜索常规文件，返回 "路径:行号: 内容"。
// 匹配数设上限，避免一个宽泛的模式把上下文冲垮。
func grepFiles(root, pattern string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	const maxMatches = 100
	const maxFileSize = 2 << 20

	var out []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] && path != root {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFileSize {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer func() { _ = f.Close() }()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		line := 0
		for scanner.Scan() {
			line++
			text := scanner.Text()
			if !re.MatchString(text) {
				continue
			}
			if len(text) > 300 {
				text = text[:300] + "…"
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				rel = path
			}
			out = append(out, fmt.Sprintf("%s:%d: %s", rel, line, strings.TrimRight(text, "\r")))
			if len(out) >= maxMatches {
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
