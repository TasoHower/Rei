package skill

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SkillJob describes a shell script run inside a skill bundle directory.
type SkillJob struct {
	WorkingDir string
	Entry      string
	Args       []string
	Context    map[string]any
}

// SkillJobRunner runs skill jobs under timeout policy.
type SkillJobRunner interface {
	Run(ctx context.Context, job SkillJob) (stdout string, err error)
}

// ShellSkillJobRunner runs scripts with /bin/sh. Only relative Entry paths resolved under WorkingDir are allowed.
type ShellSkillJobRunner struct {
	// Timeout caps each invocation. Zero defaults to one minute.
	Timeout time.Duration
}

// Run executes /bin/sh with the resolved script path and arguments.
func (r *ShellSkillJobRunner) Run(ctx context.Context, job SkillJob) (string, error) {
	if job.WorkingDir == "" {
		return "", fmt.Errorf("skill job: working directory is required")
	}
	if job.Entry == "" {
		return "", fmt.Errorf("skill job: entry script is required")
	}
	scriptAbs, err := scriptPathUnderBundle(job.WorkingDir, job.Entry)
	if err != nil {
		return "", err
	}

	d := r.Timeout
	if d <= 0 {
		d = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()

	args := append([]string{scriptAbs}, job.Args...)
	cmd := exec.CommandContext(ctx, "/bin/sh", args...)
	cmd.Dir = job.WorkingDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func scriptPathUnderBundle(bundleRoot, scriptRel string) (string, error) {
	cleanRel := filepath.Clean(scriptRel)
	if cleanRel == "." || cleanRel == ".." {
		return "", fmt.Errorf("invalid script path")
	}
	if filepath.IsAbs(cleanRel) {
		return "", fmt.Errorf("script path must be relative")
	}
	if strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("script path escapes bundle")
	}

	absBundle, err := filepath.Abs(bundleRoot)
	if err != nil {
		return "", err
	}
	full := filepath.Join(absBundle, cleanRel)
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	prefix := absBundle + string(filepath.Separator)
	if absFull != absBundle && !strings.HasPrefix(absFull, prefix) {
		return "", fmt.Errorf("script path escapes bundle")
	}
	return absFull, nil
}
