package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// RunResult is the captured outcome of a short, non-interactive command.
type RunResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Code     int    `json:"code"`
	Duration int64  `json:"duration"`
}

// run executes name with args in dir and captures its output.
func run(dir string, timeout time.Duration, stdin []byte, name string, args ...string) (RunResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "TERM=dumb")
	hideWindow(cmd)
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

// php runs the configured PHP binary in dir.
func php(dir string, timeout time.Duration, stdin []byte, args ...string) (RunResult, error) {
	return run(dir, timeout, stdin, store.PHP(), args...)
}

func artisan(dir string, timeout time.Duration, args ...string) (RunResult, error) {
	return php(dir, timeout, nil, append([]string{"artisan", "--no-interaction", "--no-ansi"}, args...)...)
}

func git(dir string, args ...string) (string, error) {
	res, err := run(dir, 30*time.Second, nil, "git", args...)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return res.Stdout, errors.New(strings.TrimSpace(firstNonEmpty(res.Stderr, res.Stdout)))
	}
	return res.Stdout, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func which(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// startDetached launches a program without waiting, e.g. an editor.
func startDetached(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if runtime.GOOS == "windows" && (strings.HasSuffix(strings.ToLower(name), ".cmd") || strings.HasSuffix(strings.ToLower(name), ".bat")) {
		hideWindow(cmd)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func itoa(i int) string { return strconv.Itoa(i) }
