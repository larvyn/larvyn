package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"larvyn/internal/api"
)

func TestPlaybooksThroughTheAPI(t *testing.T) {
	main, _ := gitRepo(t)
	p := addProject(t, main)
	c := api.Caller{Cwd: main}
	if err := os.WriteFile(filepath.Join(main, "composer.json"), []byte(`{"require-dev": {"phpunit/phpunit": "^11"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := command(c, "playbooks")
	var rows PlaybookRows
	json.Unmarshal(r.Result, &rows)
	if r.Error != "" || len(rows) < 5 || !strings.Contains(r.Text, "crud") || !strings.Contains(r.Text, "Model=…") {
		t.Fatalf("playbooks: %+v", r)
	}

	r = command(c, "playbook", "crud", "Model=Invoice")
	var pb PlaybookPrompt
	json.Unmarshal(r.Result, &pb)
	if r.Error != "" || !strings.Contains(pb.Prompt, "CRUD for a Invoice resource") {
		t.Fatalf("playbook: %+v", r)
	}
	// What Larvyn knows about the project comes first.
	if !strings.HasPrefix(pb.Prompt, "Project (from Larvyn): "+p.Name) {
		t.Errorf("context: %q", pb.Prompt)
	}

	// No Pest in composer.json: the tests stay PHPUnit.
	if !strings.Contains(pb.Prompt, "use PHPUnit, not Pest") {
		t.Errorf("phpunit note: %q", pb.Prompt)
	}

	if r := command(c, "playbook", "crud"); !strings.Contains(r.Error, "Model=") {
		t.Errorf("missing value: %+v", r)
	}
	if r := command(c, "playbook", "crud", "Invoice"); !strings.Contains(r.Error, "Name=value") {
		t.Errorf("value without a name: %+v", r)
	}
	if r := command(c, "playbook", "nope"); !strings.Contains(r.Error, "no playbook") {
		t.Errorf("unknown: %+v", r)
	}
	r = command(c, "playbook", "plan-feature", "Feature=team invitations")
	if !strings.Contains(r.Text, "larvyn specs draft") {
		t.Errorf("a spec playbook points at the spec workflow: %+v", r)
	}
}
