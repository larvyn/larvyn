package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"

	"larvyn/internal/api"
)

// `larvyn mcp` is an MCP server on stdin and stdout, for agents that use
// MCP rather than the CLI: `claude mcp add larvyn -- larvyn mcp`. Its tools
// are the app's API methods, read from the running app, so they are the
// CLI's commands under another name (terminal.read → terminal_read). Each
// call goes to the app like a CLI command, from this process's terminal
// (LARVYN_SESSION) and folder.

// protocolVersions are the MCP versions this server speaks; it answers a
// client with its own when it is one of them, else with the newest.
var protocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// mcpServer answers one client. Calls run side by side, since a wait can
// take minutes; cancels ends one.
type mcpServer struct {
	caller api.Caller
	out    io.Writer
	outMu  sync.Mutex
	mu     sync.Mutex
	calls  map[string]context.CancelFunc
	wg     sync.WaitGroup
	// dial sends a request to the app; tests replace it.
	dial func(context.Context, api.Request) (api.Response, error)
}

func serveMCP(in io.Reader, out io.Writer, caller api.Caller) int {
	s := &mcpServer{caller: caller, out: out, calls: map[string]context.CancelFunc{}, dial: api.DialContext}
	r := bufio.NewReaderSize(in, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			s.handle(line)
		}
		if err != nil {
			break
		}
	}
	// stdin closed: the client is gone and can't take an answer, so the
	// calls still running (a wait can last minutes) are stopped, in the app
	// too.
	s.mu.Lock()
	for _, cancel := range s.calls {
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return 0
}

func (s *mcpServer) send(m rpcMessage) {
	m.JSONRPC = "2.0"
	b, _ := json.Marshal(m)
	s.outMu.Lock()
	defer s.outMu.Unlock()
	_, _ = s.out.Write(append(b, '\n'))
}

func (s *mcpServer) reply(id json.RawMessage, result any) {
	s.send(rpcMessage{ID: id, Result: result})
}

func (s *mcpServer) fail(id json.RawMessage, code int, msg string) {
	s.send(rpcMessage{ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (s *mcpServer) handle(line []byte) {
	var m rpcMessage
	if err := json.Unmarshal(line, &m); err != nil {
		s.fail(json.RawMessage("null"), -32700, "parse error: "+err.Error())
		return
	}
	notification := len(m.ID) == 0
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(m.Params, &p)
		version := protocolVersions[0]
		for _, v := range protocolVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		appVersion := "unknown"
		if resp, err := s.dial(context.Background(), api.Request{Method: "version"}); err == nil && resp.Error == "" {
			_ = json.Unmarshal(resp.Result, &appVersion)
		}
		s.reply(m.ID, map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}, "prompts": map[string]any{}},
			"serverInfo":      map[string]any{"name": "larvyn", "version": appVersion},
			"instructions":    "Tools to see and drive the Larvyn app the agent runs in: its project and worktree (context), terminals, the dev stack, artisan, tests, logs, models and database, locks and other agents. Its prompts are Laravel playbooks (CRUD, tests, N+1 queries, security review…) filled in for the project. Larvyn must be running.",
		})
	case "ping":
		s.reply(m.ID, map[string]any{})
	case "tools/list":
		tools, err := s.tools()
		if err != nil {
			s.fail(m.ID, -32603, err.Error())
			return
		}
		s.reply(m.ID, map[string]any{"tools": tools})
	case "prompts/list":
		prompts, err := s.prompts()
		if err != nil {
			s.fail(m.ID, -32603, err.Error())
			return
		}
		s.reply(m.ID, map[string]any{"prompts": prompts})
	case "prompts/get":
		res, code, err := s.prompt(m.Params)
		if err != nil {
			s.fail(m.ID, code, err.Error())
			return
		}
		s.reply(m.ID, res)
	case "tools/call":
		if notification {
			// Without an id there is no one to answer.
			return
		}
		s.wg.Add(1)
		ctx, cancel := context.WithCancel(context.Background())
		s.mu.Lock()
		s.calls[string(m.ID)] = cancel
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			defer func() {
				s.mu.Lock()
				delete(s.calls, string(m.ID))
				s.mu.Unlock()
				cancel()
			}()
			s.call(ctx, m)
		}()
	case "notifications/cancelled":
		if !notification {
			s.fail(m.ID, -32600, "notifications/cancelled is a notification: it takes no id")
			return
		}
		var p struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(m.Params, &p) != nil || len(p.RequestID) == 0 {
			return
		}
		s.mu.Lock()
		if cancel := s.calls[string(p.RequestID)]; cancel != nil {
			cancel()
		}
		s.mu.Unlock()
	default:
		if !notification {
			s.fail(m.ID, -32601, "method not found: "+m.Method)
		}
	}
}

// skipped are methods that make no sense as tools: the playbooks are the
// server's prompts.
var skipped = map[string]bool{"api.list": true, "playbooks": true, "playbook": true}

// toolName is a method's name as a tool: MCP clients take letters, digits,
// _ and - only.
func toolName(method string) string { return strings.ReplaceAll(method, ".", "_") }

func (s *mcpServer) methods() ([]api.Method, error) {
	resp, err := s.dial(context.Background(), api.Request{Help: true})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var out []api.Method
	for _, m := range resp.Usage {
		if !skipped[m.Name] {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *mcpServer) tools() ([]map[string]any, error) {
	methods, err := s.methods()
	if err != nil {
		return nil, err
	}
	tools := []map[string]any{}
	for _, m := range methods {
		props := map[string]any{}
		required := []string{}
		for _, p := range m.Params {
			prop := schemaFor(p.Type)
			if p.Doc != "" {
				prop["description"] = p.Doc
			}
			props[p.Name] = prop
			if !p.Optional {
				required = append(required, p.Name)
			}
		}
		// readOnlyHint lets a client run the tools that only read without
		// asking each time. The others keep MCP's default, destructive:
		// artisan and terminal_write can do anything.
		annotations := map[string]any{"readOnlyHint": m.ReadOnly}
		tools = append(tools, map[string]any{
			"name":        toolName(m.Name),
			"description": m.Doc,
			"inputSchema": map[string]any{"type": "object", "properties": props, "required": required},
			"annotations": annotations,
		})
	}
	return tools, nil
}

// schemaFor is the JSON schema of a param type as api describes it.
func schemaFor(t string) map[string]any {
	switch {
	case t == "string":
		return map[string]any{"type": "string"}
	case t == "bool":
		return map[string]any{"type": "boolean"}
	case t == "int":
		return map[string]any{"type": "integer"}
	case t == "number":
		return map[string]any{"type": "number"}
	case strings.HasPrefix(t, "[]"):
		return map[string]any{"type": "array", "items": schemaFor(t[2:])}
	}
	return map[string]any{}
}

func (s *mcpServer) call(ctx context.Context, m rpcMessage) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(m.Params, &p); err != nil || p.Name == "" {
		s.fail(m.ID, -32602, "tools/call needs a name")
		return
	}
	methods, err := s.methods()
	if err != nil {
		s.reply(m.ID, toolResult(err.Error(), true))
		return
	}
	method := ""
	for _, x := range methods {
		if toolName(x.Name) == p.Name {
			method = x.Name
		}
	}
	if method == "" {
		s.fail(m.ID, -32602, "unknown tool "+p.Name)
		return
	}
	resp, err := s.dial(ctx, api.Request{Method: method, Params: p.Arguments, Caller: s.caller})
	switch {
	case ctx.Err() != nil:
		// Cancelled: the client expects no answer.
		return
	case err != nil:
		s.reply(m.ID, toolResult(err.Error(), true))
	case resp.Error != "":
		s.reply(m.ID, toolResult(resp.Error, true))
	default:
		text := resp.Text
		if text == "" {
			text = Human(resp.Result)
		}
		if resp.Exit != 0 {
			// The call worked; what it reports failed (tests, a wait).
			text = strings.TrimRight(text, "\n") + fmt.Sprintf("\n\n(exit code %d)", resp.Exit)
		}
		s.reply(m.ID, toolResult(strings.TrimRight(text, "\n"), false))
	}
}

// prompts are the playbooks as MCP prompts: a {Model} in a playbook is an
// argument.
func (s *mcpServer) prompts() ([]map[string]any, error) {
	resp, err := s.dial(context.Background(), api.Request{Method: "playbooks", Caller: s.caller})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var books []struct {
		ID    string   `json:"id"`
		Title string   `json:"title"`
		Text  string   `json:"text"`
		Args  []string `json:"args"`
	}
	if err := json.Unmarshal(resp.Result, &books); err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, b := range books {
		args := []map[string]any{}
		for _, a := range b.Args {
			args = append(args, map[string]any{"name": a, "description": "the " + strings.ToLower(a) + " to put in the playbook", "required": true})
		}
		out = append(out, map[string]any{"name": b.ID, "title": b.Title, "description": b.Text, "arguments": args})
	}
	return out, nil
}

// prompt fills a playbook in for the caller's project. code is the JSON-RPC
// error code when it fails: -32602 for a wrong name or a missing argument.
func (s *mcpServer) prompt(params json.RawMessage) (map[string]any, int, error) {
	var p struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, -32602, fmt.Errorf("prompts/get: invalid params (arguments must be strings): %v", err)
	}
	if p.Name == "" {
		return nil, -32602, fmt.Errorf("prompts/get needs a name")
	}
	// Sorted: the same arguments make the same request every time.
	values := []string{}
	for _, k := range slices.Sorted(maps.Keys(p.Arguments)) {
		values = append(values, k+"="+p.Arguments[k])
	}
	args, _ := json.Marshal(map[string]any{"id": p.Name, "values": values})
	resp, err := s.dial(context.Background(), api.Request{Method: "playbook", Params: args, Caller: s.caller})
	if err != nil {
		return nil, -32603, err
	}
	if resp.Error != "" {
		return nil, -32602, fmt.Errorf("%s", resp.Error)
	}
	var pb struct {
		Title  string `json:"title"`
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(resp.Result, &pb); err != nil {
		return nil, -32603, err
	}
	return map[string]any{
		"description": pb.Title,
		"messages":    []map[string]any{{"role": "user", "content": map[string]any{"type": "text", "text": pb.Prompt}}},
	}, 0, nil
}

func toolResult(text string, isError bool) map[string]any {
	if text == "" {
		text = "done"
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isError}
}

// mcpMain runs the server on this process's stdin and stdout.
func mcpMain(caller api.Caller) int { return serveMCP(os.Stdin, os.Stdout, caller) }
