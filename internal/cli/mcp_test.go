package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"larvyn/internal/api"
)

// mcpClient talks to serveMCP through pipes.
type mcpClient struct {
	t    *testing.T
	in   *io.PipeWriter
	out  chan map[string]any
	done chan int
}

func newMCP(t *testing.T) *mcpClient {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &mcpClient{t: t, in: inW, out: make(chan map[string]any, 16), done: make(chan int, 1)}
	go func() {
		c.done <- serveMCP(inR, outW, api.Caller{Session: "me"})
		outW.Close()
	}()
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var m map[string]any
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Errorf("not JSON on stdout: %q", sc.Text())
				continue
			}
			c.out <- m
		}
		close(c.out)
	}()
	t.Cleanup(func() { inW.Close() })
	return c
}

func (c *mcpClient) send(id any, method string, params any) {
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		m["id"] = id
	}
	if params != nil {
		m["params"] = params
	}
	b, _ := json.Marshal(m)
	c.in.Write(append(b, '\n'))
}

func (c *mcpClient) next() map[string]any {
	c.t.Helper()
	select {
	case m := <-c.out:
		return m
	case <-time.After(5 * time.Second):
		c.t.Fatal("no answer")
	}
	return nil
}

func (c *mcpClient) call(id int, tool string, args any) map[string]any {
	c.t.Helper()
	c.send(id, "tools/call", map[string]any{"name": tool, "arguments": args})
	m := c.next()
	if m["id"] != float64(id) {
		c.t.Fatalf("answer to %v, want %d: %v", m["id"], id, m)
	}
	return m
}

func text(m map[string]any) (string, bool) {
	res, _ := m["result"].(map[string]any)
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		return "", false
	}
	first, _ := content[0].(map[string]any)
	s, _ := first["text"].(string)
	isErr, _ := res["isError"].(bool)
	return s, isErr
}

func TestMCPInitializeAndTools(t *testing.T) {
	c := newMCP(t)
	c.send(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test"}})
	init := c.next()["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" || init["serverInfo"].(map[string]any)["name"] != "larvyn" {
		t.Fatalf("initialize: %v", init)
	}
	c.send(nil, "notifications/initialized", nil)
	c.send(2, "tools/list", nil)
	tools := c.next()["result"].(map[string]any)["tools"].([]any)
	var add map[string]any
	for _, x := range tools {
		tool := x.(map[string]any)
		// Clients may run the read-only tools without asking.
		readOnly := tool["annotations"].(map[string]any)["readOnlyHint"]
		if want := tool["name"] == "demo_whoami"; readOnly != want {
			t.Errorf("%s readOnlyHint = %v", tool["name"], readOnly)
		}
		if tool["name"] == "api_list" {
			t.Error("api.list is listed as a tool")
		}
		if tool["name"] == "demo_add" {
			add = tool
		}
	}
	if add == nil {
		t.Fatalf("no demo_add in %v", tools)
	}
	schema, _ := json.Marshal(add["inputSchema"])
	want := `{"properties":{"a":{"description":"first","type":"integer"},"b":{"description":"second","type":"integer"}},"required":["a"],"type":"object"}`
	if string(schema) != want {
		t.Errorf("schema %s", schema)
	}
	// An unknown protocol version gets the newest.
	c.send(3, "initialize", map[string]any{"protocolVersion": "1999-01-01"})
	if v := c.next()["result"].(map[string]any)["protocolVersion"]; v != protocolVersions[0] {
		t.Errorf("version %v", v)
	}
}

func TestMCPCalls(t *testing.T) {
	c := newMCP(t)
	if got, isErr := text(c.call(1, "demo_add", map[string]any{"a": 2, "b": 3})); got != "5" || isErr {
		t.Errorf("add: %q %v", got, isErr)
	}
	if got, _ := text(c.call(2, "demo_whoami", map[string]any{})); got != "me" {
		t.Errorf("caller: %q", got)
	}
	// A call that fails is a tool error, for the model to read.
	if got, isErr := text(c.call(3, "demo_add", map[string]any{})); !isErr || !strings.Contains(got, "missing a") {
		t.Errorf("missing arg: %q %v", got, isErr)
	}
	// A result that reports a failure says so, but the tool worked.
	if got, isErr := text(c.call(4, "demo_report", nil)); isErr || got != "2 tests failed\n\n(exit code 3)" {
		t.Errorf("report: %q %v", got, isErr)
	}
	if m := c.call(5, "nope", nil); m["error"] == nil {
		t.Errorf("unknown tool: %v", m)
	}
	c.send(6, "resources/list", nil)
	if m := c.next(); m["error"].(map[string]any)["code"] != float64(-32601) {
		t.Errorf("unknown method: %v", m)
	}
	// An unknown notification gets no answer.
	c.send(nil, "notifications/whatever", nil)
	c.send(7, "ping", nil)
	if m := c.next(); m["id"] != float64(7) {
		t.Errorf("ping: %v", m)
	}
}

func TestMCPClosedInputStopsTheCalls(t *testing.T) {
	c := newMCP(t)
	c.send(1, "tools/call", map[string]any{"name": "demo_slow"})
	<-slowStarted
	c.in.Close()
	select {
	case cancelled := <-slowCancelled:
		if !cancelled {
			t.Fatal("the call ran to its end")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the call kept running after stdin closed")
	}
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the server didn't exit")
	}
}

func TestMCPCancelEndsTheCall(t *testing.T) {
	c := newMCP(t)
	c.send(1, "tools/call", map[string]any{"name": "demo_slow"})
	<-slowStarted
	// Malformed cancels change nothing: one with an id gets an error, one
	// without a requestId is ignored.
	c.send(9, "notifications/cancelled", map[string]any{"requestId": 1})
	if m := c.next(); m["id"] != float64(9) || m["error"] == nil {
		t.Errorf("cancel with an id: %v", m)
	}
	c.send(nil, "notifications/cancelled", map[string]any{})
	select {
	case <-slowCancelled:
		t.Fatal("a malformed cancel stopped the call")
	case <-time.After(200 * time.Millisecond):
	}
	c.send(nil, "notifications/cancelled", map[string]any{"requestId": 1})
	select {
	case cancelled := <-slowCancelled:
		if !cancelled {
			t.Fatal("the call ran to its end")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the app never saw the cancel")
	}
	// No answer to a cancelled request; the next one still works.
	c.send(2, "ping", nil)
	if m := c.next(); m["id"] != float64(2) {
		t.Errorf("after cancel: %v", m)
	}
}
