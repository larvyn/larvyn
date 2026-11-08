package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"larvyn/internal/api"
	"larvyn/internal/plugins"
	"larvyn/internal/stack"
	"larvyn/internal/terminal"
)

// The dev stack through the API: the same items and checks as the sidebar.
// Starting one doesn't open the console drawer or change the view.
func init() {
	stack.Tail = func(id string, n int) string {
		out, _ := terminal.Sessions.Output(id)
		return lastLines(out, n)
	}
	const stackDoc = " (the project's stack, which runs in its main folder)"
	api.Register("stack.list", "List the dev stack: HTTP server, Vite, queue…, and what runs"+stackDoc, apiStackList)
	api.Register("stack.start", "Start a stack item"+stackDoc, apiStackStart,
		"key: the item, as stack list shows it (serve, vite, queue…)",
		"wait?: then wait until it is ready, as stack wait does",
		"timeout?: seconds to wait, 60 by default")
	api.Register("stack.stop", "Stop a stack item", apiStackStop, "key: the item")
	api.Register("stack.restart", "Restart a stack item", apiStackRestart,
		"key: the item",
		"wait?: then wait until it is ready",
		"timeout?: seconds to wait, 60 by default")
	api.Register("stack.wait", "Wait until a stack item is ready; exits 1 if it stops or the timeout passes", apiStackWait,
		"key: the item",
		"port?: ready when this port on 127.0.0.1 answers (the HTTP servers' own port by default)",
		"line?: ready when a line of its output matches this regular expression (Vite's \"ready in\"… by default)",
		"timeout?: seconds, 60 by default")
}

// StackRow is a line of `larvyn stack list`.
type StackRow struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	URL      string `json:"url,omitempty"`
	Terminal string `json:"terminal,omitempty"`
}

func stackRow(it stack.StackItem) StackRow {
	r := StackRow{Key: it.Key, Title: it.Label, URL: it.URL, Status: "stopped"}
	switch {
	case it.ServedBy != "":
		r.Status = "served by " + it.ServedBy
	case it.Session != nil && it.Session.Running:
		r.Status = "running"
	case it.Session != nil && !it.Session.Stopped && it.Session.ExitCode != 0:
		r.Status = fmt.Sprintf("crashed (exit %d)", it.Session.ExitCode)
	case !it.Available:
		r.Status = "unavailable: " + it.Reason
	}
	if it.Session != nil {
		r.Terminal = it.Session.ID
	}
	return r
}

func apiStackList(c api.Caller) ([]StackRow, error) {
	p, _, _, err := callerProject(c)
	if err != nil {
		return nil, err
	}
	items, err := (&stack.StackService{}).Items(p.ID)
	if err != nil {
		return nil, err
	}
	out := []StackRow{}
	for _, it := range items {
		out = append(out, stackRow(it))
	}
	return out, nil
}

// stackItem finds an item by key, or by the end of one: "mail" for the
// project file's "project:mail".
func stackItem(c api.Caller, key string) (projectID string, it stack.StackItem, err error) {
	p, _, _, err := callerProject(c)
	if err != nil {
		return "", it, err
	}
	items, err := (&stack.StackService{}).Items(p.ID)
	if err != nil {
		return "", it, err
	}
	var found []stack.StackItem
	for _, x := range items {
		if x.Key == key {
			return p.ID, x, nil
		}
		if strings.HasSuffix(x.Key, ":"+key) {
			found = append(found, x)
		}
	}
	if len(found) == 1 {
		return p.ID, found[0], nil
	}
	var keys []string
	for _, x := range items {
		keys = append(keys, x.Key)
	}
	if len(found) > 1 {
		return "", it, fmt.Errorf("%q matches several items: %s", key, strings.Join(keys, ", "))
	}
	return "", it, fmt.Errorf("no stack item %q; there are %s", key, strings.Join(keys, ", "))
}

func timeoutOf(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = 60
	}
	return time.Duration(min(seconds, 1800)) * time.Second
}

// startError says what the UI would have asked.
func startError(err error) error {
	var confirm plugins.ConfirmError
	if errors.As(err, &confirm) {
		return fmt.Errorf("%s runs %q from the %s plugin, which you have to allow once: start it in Larvyn's sidebar", confirm.Title, confirm.Line, confirm.PluginName)
	}
	return err
}

func apiStackStart(c api.Caller, key string, wait bool, timeout int) (any, error) {
	return startStack(c, key, wait, timeout, false)
}

func apiStackRestart(c api.Caller, key string, wait bool, timeout int) (any, error) {
	return startStack(c, key, wait, timeout, true)
}

func startStack(c api.Caller, key string, wait bool, timeout int, restart bool) (any, error) {
	projectID, it, err := stackItem(c, key)
	if err != nil {
		return nil, err
	}
	if it.ServedBy == "" {
		s := &stack.StackService{}
		if restart {
			_, err = s.Restart(projectID, it.Key)
		} else {
			_, err = s.Start(projectID, it.Key)
		}
		if err != nil {
			return nil, startError(err)
		}
	}
	if wait {
		return stack.WaitReady(c.Ctx, projectID, it.Key, stack.Ready{}, timeoutOf(timeout))
	}
	_, it, err = stackItem(c, it.Key)
	return stackRow(it), err
}

func apiStackStop(c api.Caller, key string) error {
	projectID, it, err := stackItem(c, key)
	if err != nil {
		return err
	}
	if err := (&stack.StackService{}).Stop(projectID, it.Key); err != nil {
		return err
	}
	// Stop only signals it: return once it is gone, so the next command
	// (a start on the same port) doesn't race it. A program that ignores
	// the signal is killed after about 3 s.
	for deadline := time.Now().Add(6 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if s := terminal.Find(projectID, "service", it.Key); s == nil || !s.Running {
			return nil
		}
	}
	return fmt.Errorf("%s is still running", it.Label)
}

func apiStackWait(c api.Caller, key string, port int, line string, timeout int) (stack.WaitResult, error) {
	projectID, it, err := stackItem(c, key)
	if err != nil {
		return stack.WaitResult{}, err
	}
	return stack.WaitReady(c.Ctx, projectID, it.Key, stack.Ready{Port: port, Line: line}, timeoutOf(timeout))
}
