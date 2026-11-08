package stack

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// Ready says how to tell that a stack item is up: a port on this machine
// answers, or a line matching a regular expression shows in its output.
// With neither, an item is ready once it has run for a moment without
// exiting.
type Ready struct {
	Port int    `json:"port,omitempty"`
	Line string `json:"line,omitempty"`
}

// readyLines are what Larvyn's own items print once they work.
var readyLines = map[string]string{
	"vite":     `(?i)ready in|Local:\s+http`,
	"queue":    `(?i)processing jobs|horizon started`,
	"schedule": `(?i)running scheduled tasks`,
	"reverb":   `(?i)starting server`,
}

// DefaultReady is how an item is checked when the caller names nothing:
// the port of its URL for the HTTP servers, a known line for the others.
func DefaultReady(it StackItem) Ready {
	if it.Key == "serve" || it.Key == "octane" {
		if u, err := url.Parse(it.URL); err == nil {
			if n, err := strconv.Atoi(u.Port()); err == nil {
				return Ready{Port: n}
			}
		}
	}
	if it.Plugin == "" && !it.Shared {
		return Ready{Line: readyLines[it.Key]}
	}
	return Ready{}
}

// WaitResult is how a wait ended.
type WaitResult struct {
	Key      string  `json:"key"`
	Title    string  `json:"title"`
	Ready    bool    `json:"ready"`
	By       string  `json:"by"`
	Seconds  float64 `json:"seconds"`
	URL      string  `json:"url,omitempty"`
	Terminal string  `json:"terminal,omitempty"`
	// Exited is set when the item stopped before it got ready, with its
	// exit code; Tail is its last lines then, or at the timeout.
	Exited bool   `json:"exited,omitempty"`
	Code   int    `json:"code,omitempty"`
	Tail   string `json:"tail,omitempty"`
}

// Text is what larvyn stack wait prints.
func (r WaitResult) Text() string {
	var b strings.Builder
	switch {
	case r.Ready:
		fmt.Fprintf(&b, "%s is ready (%s) after %.1fs", r.Title, r.By, r.Seconds)
		if r.URL != "" {
			b.WriteString(": " + r.URL)
		}
	case r.Exited:
		fmt.Fprintf(&b, "%s stopped with exit code %d before it was ready", r.Title, r.Code)
	default:
		fmt.Fprintf(&b, "%s is not ready (%s) after %.0fs", r.Title, r.By, r.Seconds)
	}
	b.WriteString("\n")
	if !r.Ready && r.Tail != "" {
		b.WriteString("\nLast lines:\n" + r.Tail + "\n")
	}
	return b.String()
}

// ExitCode fails the command when the item did not get ready.
func (r WaitResult) ExitCode() int {
	if r.Ready {
		return 0
	}
	return 1
}

// settle is how long an item with nothing to check must run to count as up.
const settle = 2 * time.Second

// Tail returns a terminal's last lines as plain text; main sets it, as the
// API reads terminals the same way.
var Tail = func(id string, n int) string { return "" }

// WaitReady waits until a project's stack item is ready, it stops, the
// timeout passes or ctx ends. A zero Ready uses DefaultReady. The item must
// have been started.
func WaitReady(ctx context.Context, projectID, key string, ready Ready, timeout time.Duration) (WaitResult, error) {
	p, ok := store.FindProject(projectID)
	if !ok {
		return WaitResult{}, errors.New("project not found")
	}
	var item *StackItem
	for _, it := range stackItems(p) {
		if it.Key == key {
			item = &it
			break
		}
	}
	if item == nil {
		return WaitResult{}, errors.New("unknown stack item " + key)
	}
	res := WaitResult{Key: key, Title: item.Label, URL: item.URL}
	if item.ServedBy != "" {
		res.Ready, res.By = true, "served by "+item.ServedBy
		return res, nil
	}
	if ready == (Ready{}) {
		ready = DefaultReady(*item)
	}
	var line *regexp.Regexp
	if ready.Line != "" {
		re, err := regexp.Compile(ready.Line)
		if err != nil {
			return WaitResult{}, fmt.Errorf("--line: %v", err)
		}
		line = re
	}
	switch {
	case ready.Port > 0:
		res.By = fmt.Sprintf("port %d answers", ready.Port)
	case line != nil:
		res.By = fmt.Sprintf("output matches %q", ready.Line)
	default:
		res.By = fmt.Sprintf("running for %s", settle)
	}
	start := time.Now()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		sess := terminal.Find(projectID, "service", key)
		if sess == nil {
			return res, fmt.Errorf("%s is not running: start it first (larvyn stack start %s)", item.Label, key)
		}
		res.Terminal = sess.ID
		res.Seconds = time.Since(start).Seconds()
		if !sess.Running {
			res.Exited, res.Code, res.Tail = true, sess.ExitCode, Tail(sess.ID, 20)
			return res, nil
		}
		switch {
		case ready.Port > 0:
			if c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ready.Port)), 300*time.Millisecond); err == nil {
				c.Close()
				res.Ready = true
			}
		case line != nil:
			res.Ready = line.MatchString(Tail(sess.ID, 400))
		default:
			res.Ready = time.Since(time.UnixMilli(sess.StartedAt)) >= settle
		}
		if res.Ready {
			return res, nil
		}
		select {
		case <-ctx.Done():
			return res, errors.New("stopped waiting")
		case <-deadline.C:
			res.Tail = Tail(sess.ID, 20)
			return res, nil
		case <-tick.C:
		}
	}
}
