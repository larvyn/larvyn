// Package cli is the larvyn command: it drives the running Larvyn app from
// a terminal, so the agents Larvyn starts can see their project, read and
// type into terminals and so on. It is a thin client: the app parses each
// command (see internal/api), so a method the app adds is a command here
// without changing this package.
//
//	larvyn context
//	larvyn terminal read <id> --lines 50
//	larvyn api list
//	larvyn api call terminal.read '{"id":"…","lines":50}'
//	larvyn mcp    # the same methods as an MCP server on stdin/stdout (mcp.go)
//
// --json prints the result as JSON instead of text.
//
// On Linux and macOS the app's own binary runs it when given a command
// (main.go); Windows builds the app as a GUI program, which can't print to
// a terminal, so there it is the separate cmd/larvyn.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"larvyn/internal/api"
)

// IsCommand reports whether the app was started as the CLI: with a command
// rather than none (macOS may pass -psn_… to an app it opens).
func IsCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "-h", "--help", "--json":
		return true
	}
	return !strings.HasPrefix(args[0], "-")
}

// Run runs a command and returns the exit code.
func Run(argv []string, stdout, stderr io.Writer) int {
	// --json and --help are left in argv for the app, which tells them from
	// a command's own (larvyn artisan route:list --json passes it to artisan).
	// help, -h or --help first asks for usage.
	asJSON := false
	words := argv
	if len(words) > 0 && (words[0] == "-h" || words[0] == "--help") {
		words = append([]string{"help"}, words[1:]...)
	}
	if len(words) > 0 && (words[0] == "help" || (len(words) > 1 && words[0] == "api" && words[1] == "call")) {
		var kept []string
		for _, w := range words {
			if w == "--json" {
				asJSON = true
			} else if w != "-h" && w != "--help" {
				kept = append(kept, w)
			}
		}
		words = kept
	}
	cwd, _ := os.Getwd()
	req := api.Request{Caller: api.Caller{Session: os.Getenv("LARVYN_SESSION"), Cwd: cwd}}
	if len(words) == 1 && words[0] == "mcp" {
		return mcpMain(req.Caller)
	}
	if len(words) == 2 && words[0] == "agents" && words[1] == "hook" {
		return runHook(req.Caller, stdout)
	}
	switch {
	case len(words) == 0 || words[0] == "help":
		// The app answers a request with no command with every method.
		req.Help = true
		if len(words) > 1 {
			req.Argv = words[1:]
		}
	case len(words) >= 2 && words[0] == "api" && words[1] == "call":
		if len(words) < 3 || len(words) > 4 {
			fmt.Fprintln(stderr, "usage: larvyn api call <method> ['{\"param\": value}']")
			return 2
		}
		req.Method = words[2]
		if len(words) == 4 {
			if !json.Valid([]byte(words[3])) {
				fmt.Fprintln(stderr, `larvyn: the params must be JSON, like '{"id":"abc"}'`)
				return 2
			}
			req.Params = json.RawMessage(words[3])
		}
	default:
		req.Argv = words
	}
	resp, err := api.Dial(req)
	if err != nil {
		fmt.Fprintln(stderr, "larvyn:", err)
		return 1
	}
	if resp.Error != "" {
		fmt.Fprintln(stderr, "larvyn:", resp.Error)
		return 1
	}
	asJSON = asJSON || resp.JSON
	if resp.Usage != nil {
		if asJSON {
			b, _ := json.MarshalIndent(resp.Usage, "", "  ")
			fmt.Fprintln(stdout, string(b))
			return 0
		}
		printUsage(stdout, resp.Usage)
		return 0
	}
	switch {
	case asJSON:
		var v any
		if json.Unmarshal(resp.Result, &v) == nil {
			b, _ := json.MarshalIndent(v, "", "  ")
			fmt.Fprintln(stdout, string(b))
		}
	case resp.Text != "":
		fmt.Fprint(stdout, resp.Text)
		if !strings.HasSuffix(resp.Text, "\n") {
			fmt.Fprintln(stdout)
		}
	default:
		fmt.Fprint(stdout, Human(resp.Result))
	}
	return resp.Exit
}

func printUsage(w io.Writer, methods []api.Method) {
	fmt.Fprintln(w, "Usage: larvyn <command> [arguments] [--option value] [--json]")
	for _, m := range methods {
		fmt.Fprintf(w, "\n  %s\n      %s\n", command(m), m.Doc)
		width := 0
		for _, p := range m.Params {
			width = max(width, len(p.Name))
		}
		for _, p := range m.Params {
			if p.Doc != "" {
				fmt.Fprintf(w, "      %-*s  %s\n", width, p.Name, p.Doc)
			}
		}
	}
	fmt.Fprintln(w, "\n  api call <method> '<json>'\n      Call a method with a JSON object of params")
	fmt.Fprintln(w, "\n  mcp\n      Serve these commands as MCP tools on stdin and stdout: claude mcp add larvyn -- larvyn mcp")
}

// command is a method as typed: "terminal read <id> [--lines int]".
func command(m api.Method) string {
	return strings.ReplaceAll(m.Name, ".", " ") + strings.TrimPrefix(m.Usage(), m.Name)
}
