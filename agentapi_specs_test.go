package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"larvyn/internal/proc"
	"larvyn/internal/terminal"
)

// The whole Elephant-Goldfish loop through the CLI, with the fake agent
// standing in for each role and the test writing what the real agents would.
func TestSpecsWorkflowThroughTheAPI(t *testing.T) {
	main, caller := fakeAgents(t)

	r := command(caller, "specs", "draft", "CSV export of the orders page")
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	var elephant AgentRow
	json.Unmarshal(r.Result, &elephant)
	if elephant.Worktree != "" {
		t.Fatalf("the Elephant works in the project, not a worktree: %+v", elephant)
	}
	if r := command(caller, "agents", "wait", elephant.ID, "--timeout", "20"); r.Exit == 1 {
		t.Fatalf("wait for the Elephant: %s %s", r.Error, r.Text)
	}
	slug := "csv-export-of-the-orders-page"
	specFile := filepath.Join(main, ".larvyn", "specs", slug+".md")
	if _, err := os.Stat(specFile); err != nil {
		t.Fatalf("no spec skeleton: %v", err)
	}
	// Specs never show up as changes in git.
	if out, _ := proc.Git(main, "status", "--porcelain", "--untracked-files=all"); strings.Contains(out, "specs") {
		t.Fatalf("spec shows in git status:\n%s", out)
	}

	r = command(caller, "specs", "list")
	var rows []SpecRow
	json.Unmarshal(r.Result, &rows)
	if len(rows) != 1 || rows[0].Slug != slug || rows[0].Status != "draft" {
		t.Fatalf("list: %+v (%s)", rows, r.Error)
	}

	// Building an unreviewed spec needs --force.
	if r = command(caller, "specs", "build", slug); !strings.Contains(r.Error, "not been reviewed") {
		t.Fatalf("build before review: %+v", r)
	}

	r = command(caller, "specs", "review", slug, "--focus", "streaming large exports")
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	var goldfish AgentRow
	json.Unmarshal(r.Result, &goldfish)
	_ = terminal.Sessions.Close(goldfish.ID)
	at := time.Now().Add(time.Second)
	review := filepath.Join(main, ".larvyn", "specs", slug+".review.md")
	os.WriteFile(review, []byte("Verdict: READY\n\nStream with a lazy cursor."), 0o644)
	os.Chtimes(review, at, at)

	r = command(caller, "specs", "show", slug, "--review")
	if !strings.HasPrefix(r.Text, "Verdict: READY") {
		t.Fatalf("show --review: %+v", r)
	}

	r = command(caller, "specs", "build", slug)
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	var builders []AgentRow
	json.Unmarshal(r.Result, &builders)
	if len(builders) != 1 || builders[0].Worktree == "" {
		t.Fatalf("builders: %+v", builders)
	}
	wt := builders[0].Worktree
	if _, err := os.Stat(filepath.Join(wt, ".larvyn", "specs", slug+".md")); err != nil {
		t.Fatalf("the worktree has no copy of the spec: %v", err)
	}
	if out, _ := proc.Git(wt, "status", "--porcelain", "--untracked-files=all"); strings.Contains(out, "specs") {
		t.Fatalf("the spec copy shows in the worktree's git status:\n%s", out)
	}

	if r = command(caller, "specs", "fix", builders[0].ID); !strings.Contains(r.Error, "no drift report") {
		t.Fatalf("fix before drift: %+v", r)
	}
	r = command(caller, "specs", "drift", builders[0].ID)
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	var auditor AgentRow
	json.Unmarshal(r.Result, &auditor)
	if auditor.Worktree != wt {
		t.Fatalf("the auditor runs in the builder's worktree: %+v", auditor)
	}
	_ = terminal.Sessions.Close(auditor.ID)
	os.WriteFile(filepath.Join(wt, ".larvyn", "specs", slug+".drift.md"), []byte("Verdict: MINOR DRIFT\n\n- [ ] the header row"), 0o644)
	if r = command(caller, "specs", "fix", builders[0].ID); r.Error != "" {
		t.Fatalf("fix: %+v", r)
	}
}
