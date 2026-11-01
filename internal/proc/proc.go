// Package proc runs external programs (PHP, artisan, git…) and finds them on
// the user's PATH, including the PATH of their login shell.
package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"larvyn/internal/store"
)

// RunResult is the captured outcome of a short, non-interactive command.
type RunResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Code     int    `json:"code"`
	Duration int64  `json:"duration"`
}

// Run executes name with args in dir and captures its output.
func Run(dir string, timeout time.Duration, stdin []byte, name string, args ...string) (RunResult, error) {
	WaitShellEnv()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "TERM=dumb")
	HideWindow(cmd)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	start := time.Now()
	err := cmd.Run()
	res := RunResult{Stdout: out.String(), Stderr: errb.String(), Duration: time.Since(start).Milliseconds()}
	if cmd.ProcessState != nil {
		res.Code = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() == context.DeadlineExceeded {
		return res, fmt.Errorf("%s timed out after %s", name, timeout)
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return res, err
	}
	return res, nil
}

// PHP runs the configured PHP binary in dir.
func PHP(dir string, timeout time.Duration, stdin []byte, args ...string) (RunResult, error) {
	return Run(dir, timeout, stdin, store.PHP(), args...)
}

// Artisan runs php artisan non-interactively in dir.
func Artisan(dir string, timeout time.Duration, args ...string) (RunResult, error) {
	return PHP(dir, timeout, nil, append([]string{"artisan", "--no-interaction", "--no-ansi"}, args...)...)
}

// Git runs git in dir and returns stdout; a non-zero exit is an error
// carrying git's message.
func Git(dir string, args ...string) (string, error) {
	res, err := Run(dir, 30*time.Second, nil, "git", args...)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return res.Stdout, errors.New(strings.TrimSpace(FirstNonEmpty(res.Stderr, res.Stdout)))
	}
	return res.Stdout, nil
}

// FirstNonEmpty returns the first value that is not blank.
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Which finds a program on PATH (after the shell PATH import), or "".
func Which(name string) string {
	WaitShellEnv()
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// StartDetached launches a program without waiting, e.g. an editor.
func StartDetached(dir, name string, args ...string) error {
	WaitShellEnv()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if runtime.GOOS == "windows" && (strings.HasSuffix(strings.ToLower(name), ".cmd") || strings.HasSuffix(strings.ToLower(name), ".bat")) {
		HideWindow(cmd)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
