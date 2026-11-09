package main

import (
	"fmt"
	"strings"

	"larvyn/internal/agents"
	"larvyn/internal/api"
	"larvyn/internal/project"
)

// Playbooks through the API, and through `larvyn mcp` as MCP prompts: the
// Agents view's Laravel playbooks, filled in for the project the caller
// works in.
func init() {
	api.Register("playbooks", "List the Laravel playbooks: ready-made tasks such as a CRUD, tests, N+1 queries or a security review", apiPlaybooks)
	api.Register("playbook", "Print a playbook's prompt, with what Larvyn knows about the project (Laravel, PHP, database, packages, branch) in front", apiPlaybook,
		"id: the playbook (larvyn playbooks lists them)",
		"values...?: its placeholders as Name=value: Model=Invoice")
	api.MarkReadOnly("playbooks", "playbook")
}

// PlaybookRows is what playbooks prints.
type PlaybookRows []agents.Playbook

func (ps PlaybookRows) Text() string {
	var b strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&b, "%-17s %s", p.ID, p.Title)
		for _, a := range p.Args {
			fmt.Fprintf(&b, "  %s=…", a)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func apiPlaybooks() PlaybookRows { return agents.AllPlaybooks() }

// PlaybookPrompt is a playbook filled in for the caller's project.
type PlaybookPrompt struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

func (p PlaybookPrompt) Text() string { return p.Prompt + "\n" }

func apiPlaybook(c api.Caller, id string, values []string) (PlaybookPrompt, error) {
	pb, ok := agents.FindPlaybook(id)
	if !ok {
		return PlaybookPrompt{}, fmt.Errorf("no playbook %q (larvyn playbooks lists them)", id)
	}
	vals := map[string]string{}
	for _, v := range values {
		k, val, ok := strings.Cut(v, "=")
		if !ok {
			return PlaybookPrompt{}, fmt.Errorf("%q: give values as Name=value", v)
		}
		vals[strings.TrimSpace(k)] = val
	}
	text, missing := pb.Fill(vals)
	if len(missing) > 0 {
		return PlaybookPrompt{}, fmt.Errorf("playbook %s needs %s=…", pb.ID, strings.Join(missing, "=… "))
	}
	head, info := playbookContext(c)
	// The playbooks say Pest; a project on PHPUnit keeps to it.
	if info.TestRunner == "phpunit" && strings.Contains(text, "Pest") {
		text += "\n\nThis project's tests use PHPUnit, not Pest: write them for PHPUnit."
	}
	if pb.Spec {
		text += "\n\nWrite the spec only, no application code. In Larvyn, `larvyn specs draft \"<feature>\"` runs this as the spec-first workflow."
	}
	return PlaybookPrompt{ID: pb.ID, Title: pb.Title, Prompt: strings.TrimSpace(head + "\n\n" + text)}, nil
}

// playbookContext is what Larvyn knows about the caller's project, for an
// agent that starts on a playbook without having looked around yet.
func playbookContext(c api.Caller) (string, project.ProjectInfo) {
	p, dir, _, err := callerProject(c)
	if err != nil {
		return "", project.ProjectInfo{}
	}
	at := p
	at.Path = appRoot(p, dir)
	info := project.Inspect(at)
	var facts []string
	// The values come from the project's own files (composer.json, .env,
	// git), so each is kept to one short line: no newlines that could pass
	// as instructions in the prompt.
	add := func(label, v string) {
		v = strings.Join(strings.Fields(v), " ")
		if r := []rune(v); len(r) > 80 {
			v = string(r[:80]) + "…"
		}
		if v != "" {
			facts = append(facts, label+v)
		}
	}
	add("", p.Name)
	if info.IsLaravel {
		add("Laravel ", info.LaravelVersion)
	}
	add("PHP ", info.PHPConstraint)
	add("database: ", info.DBConnection)
	add("tests: ", info.TestRunner)
	if len(info.Frontend) > 0 {
		add("frontend: ", strings.Join(info.Frontend, ", "))
	}
	var pkgs []string
	for _, pk := range info.Packages {
		if !pk.Dev && pk.Label != "" && len(pkgs) < 8 {
			pkgs = append(pkgs, pk.Label)
		}
	}
	if len(pkgs) > 0 {
		add("packages: ", strings.Join(pkgs, ", "))
	}
	add("branch: ", info.Branch)
	lines := []string{"Project (from Larvyn): " + strings.Join(facts, " · ")}
	if info.IsLaravel {
		lines = append(lines, "Larvyn's tools work here: larvyn test, larvyn models, larvyn db tables / db query, larvyn logs errors, larvyn artisan (the same as MCP tools).")
	}
	return strings.Join(lines, "\n"), info
}
