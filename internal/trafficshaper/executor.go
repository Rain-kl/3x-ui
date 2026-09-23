package trafficshaper

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CommandExecutor abstracts shell command execution for testing and portability.
type CommandExecutor interface {
	Execute(ctx context.Context, cmd string, args ...string) error
}

// SysExecutor executes OS commands with a per-invocation timeout limit.
type SysExecutor struct {
	timeout time.Duration
}

// NewSysExecutor creates a SysExecutor with a default 3-second execution timeout.
func NewSysExecutor() *SysExecutor {
	return &SysExecutor{timeout: 3 * time.Second}
}

// NewSysExecutorWithTimeout creates a SysExecutor with custom timeout duration.
func NewSysExecutorWithTimeout(timeout time.Duration) *SysExecutor {
	return &SysExecutor{timeout: timeout}
}

// Execute runs the command with context timeout and captures stderr/stdout on error.
func (s *SysExecutor) Execute(ctx context.Context, cmd string, args ...string) error {
	execCtx := ctx
	if s.timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}
	c := exec.CommandContext(execCtx, cmd, args...)
	output, err := c.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if len(trimmed) > 0 {
			return fmt.Errorf("%s %s failed: %w (output: %s)", cmd, strings.Join(args, " "), err, trimmed)
		}
		return fmt.Errorf("%s %s failed: %w", cmd, strings.Join(args, " "), err)
	}
	return nil
}
