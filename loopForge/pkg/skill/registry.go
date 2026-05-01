package skill

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const skillFileName = "SKILL.md"

// SkillRegistry holds loaded skills. Reads use an atomic snapshot; LoadFromPaths replaces it.
type SkillRegistry struct {
	snap atomic.Pointer[regSnapshot]
	gen  atomic.Uint64
}

type regSnapshot struct {
	byName map[string]*SkillSpec
	order  []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *SkillRegistry {
	r := &SkillRegistry{}
	r.snap.Store(&regSnapshot{byName: make(map[string]*SkillSpec)})
	return r
}

// Generation increments after each successful LoadFromPaths.
func (r *SkillRegistry) Generation() uint64 {
	if r == nil {
		return 0
	}
	return r.gen.Load()
}

// Get returns a skill by logical name.
func (r *SkillRegistry) Get(name string) (*SkillSpec, error) {
	if r == nil {
		return nil, ErrNotFound
	}
	snap := r.snap.Load()
	if snap == nil {
		return nil, ErrNotFound
	}
	spec, ok := snap.byName[name]
	if !ok || spec == nil {
		return nil, ErrNotFound
	}
	return spec, nil
}

// List returns metadata for all registered skills in stable name order.
func (r *SkillRegistry) List() []SkillMeta {
	if r == nil {
		return nil
	}
	snap := r.snap.Load()
	if snap == nil || len(snap.byName) == 0 {
		return nil
	}
	names := append([]string(nil), snap.order...)
	sort.Strings(names)
	out := make([]SkillMeta, 0, len(names))
	for _, n := range names {
		sp := snap.byName[n]
		if sp == nil {
			continue
		}
		out = append(out, SkillMeta{
			Name:        sp.Name,
			Version:     sp.Version,
			Description: sp.Description,
			SourcePath:  sp.SourcePath,
		})
	}
	return out
}

// LoadFromPaths walks each root directory in order, discovers SKILL.md files,
// and registers skills. Duplicate names keep the first occurrence (path order,
// then lexical walk order).
func (r *SkillRegistry) LoadFromPaths(ctx context.Context, paths []string) error {
	if r == nil {
		return nil
	}

	tracer := otel.Tracer("github.com/TasoHower/rei/loopForge/skill")
	ctx, span := tracer.Start(ctx, "skill.registry.load",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	var loadErr error
	defer func() {
		if loadErr != nil {
			span.RecordError(loadErr)
			span.SetStatus(codes.Error, loadErr.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}()

	next := make(map[string]*SkillSpec)
	var order []string

	for _, root := range paths {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			loadErr = err
			return err
		}
		fi, err := os.Stat(absRoot)
		if err != nil {
			loadErr = err
			return err
		}
		if !fi.IsDir() {
			loadErr = &fs.PathError{Op: "LoadFromPaths", Path: absRoot, Err: fs.ErrInvalid}
			return loadErr
		}

		walkErr := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if d.Name() != skillFileName {
				return nil
			}
			absPath, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if !isUnderRoot(absRoot, absPath) {
				return nil
			}

			_, spn := tracer.Start(ctx, "skill.parse",
				trace.WithAttributes(
					attribute.String("path", absPath),
				),
			)
			spec, perr := ParseSKILLFile(absPath)
			if perr != nil {
				spn.RecordError(perr)
				spn.SetStatus(codes.Error, perr.Error())
				spn.End()
				return perr
			}
			spn.SetAttributes(
				attribute.String("skill.name", spec.Name),
				attribute.String("skill.version", spec.Version),
				attribute.String("skill.content_hash", spec.ContentHash),
			)
			spn.SetStatus(codes.Ok, "")
			spn.End()

			if _, exists := next[spec.Name]; exists {
				slog.Debug("skill registry: duplicate name skipped (first wins)",
					"name", spec.Name,
					"path", absPath,
				)
				return nil
			}
			next[spec.Name] = spec
			order = append(order, spec.Name)

			slog.Info("skill loaded",
				"skill", spec.Name,
				"version", spec.Version,
				"source", spec.SourcePath,
				"content_hash", spec.ContentHash,
			)
			return nil
		})
		if walkErr != nil {
			loadErr = walkErr
			return walkErr
		}
	}

	r.snap.Store(&regSnapshot{byName: next, order: order})
	r.gen.Add(1)
	slog.Info("skill registry load complete",
		"skill_count", len(next),
		"generation", r.gen.Load(),
	)
	return nil
}

func isUnderRoot(absRoot, absPath string) bool {
	root := filepath.Clean(absRoot)
	p := filepath.Clean(absPath)
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
