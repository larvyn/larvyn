package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"larvyn/internal/api"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// fakeAgent works like an agent's TUI: a spinner redrawn in place while it
// works on its prompt, then "● Done: <task>", then the next task from its
// input. A task with "ask" in it ends on a question with the bell; one with
// "slow" works for 15 s.
func fakeAgent() {
	work := func(task string) {
		steps := 8
		if strings.Contains(task, "slow") {
			steps = 100
		}
		for i := range steps {
			fmt.Printf("\r\x1b[2K✻ Working… %d", i)
			time.Sleep(150 * time.Millisecond)
		}
		fmt.Print("\r\x1b[2K  read routes/web.php\r\n  edited app/Http/Controllers/LoginController.php\r\n  ran php artisan test\r\n")
		fmt.Printf("● Done: %s\r\n", task)
		if strings.Contains(task, "ask") {
			fmt.Print("\aAllow the edit? (y/n)\r\n")
		}
		if strings.Contains(task, "trust") {
			// A question with no bell, like a trust prompt.
			fmt.Print("Do you trust the contents of this project?\r\n> Yes, I trust this folder\r\n  No, exit\r\n  ↑/↓ Navigate · enter Confirm\r\n")
		}
		fmt.Print("> ")
	}
	work(strings.Join(os.Args[1:], " "))
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.NewReplacer("\x1b[200~", "", "\x1b[201~", "").Replace(sc.Text())
		if line = strings.TrimSpace(line); line != "" {
			work(line)
		}
	}
}

// fakeAgents makes the fake agent Settings' default agent, in a git project.
func fakeAgents(t *testing.T) (main string, caller api.Caller) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	before := store.CurrentSettings()
	store.Update(func(d *store.State) {
		d.Settings.CustomAgents = []store.CustomAgent{{ID: "fake", Name: "Fake Agent", Base: "cursor-agent", Command: exe, Env: map[string]string{"LARVYN_FAKE_AGENT": "1"}}}
		d.Settings.DefaultAgent = "fake"
		d.Settings.AgentPermissions = "manual"
	})
	main, _ = gitRepo(t)
	p := addProject(t, main)
	t.Cleanup(func() {
		for _, s := range terminal.Sessions.List() {
			if s.ProjectID == p.ID {
				_ = terminal.Sessions.Close(s.ID)
			}
		}
		store.Update(func(d *store.State) { d.Settings = before })
	})
	return main, api.Caller{Cwd: main}
}

func startAgent(t *testing.T, c api.Caller, prompt string) AgentRow {
	t.Helper()
	r := command(c, "agents", "start", prompt)
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	var row AgentRow
	json.Unmarshal(r.Result, &row)
	return row
}

func TestAgentsStartSendAndWait(t *testing.T) {
	_, caller := fakeAgents(t)
	row := startAgent(t, caller, "fix the login")
	if row.Agent != "Fake Agent" || row.Worktree == "" || !strings.HasPrefix(row.Branch, "larvyn/") {
		t.Fatalf("started: %+v", row)
	}

	// Right after the start it hasn't printed yet: wait must not take that
	// for done.
	r := command(caller, "agents", "wait", row.ID, "--timeout", "20")
	if r.Error != "" || r.Exit != 0 {
		t.Fatalf("wait: %+v", r)
	}
	var rep AgentReport
	json.Unmarshal(r.Result, &rep)
	if rep.Status != "idle" || !strings.Contains(rep.Screen, "● Done: fix the login") || strings.Contains(rep.Screen, "Working") {
		t.Fatalf("report: %+v", rep)
	}

	if r := command(caller, "agents", "send", row.ID, "add the tests"); r.Error != "" {
		t.Fatal(r.Error)
	}
	r = command(caller, "agents", "wait", row.ID, "--timeout", "20")
	json.Unmarshal(r.Result, &rep)
	if r.Exit != 0 || !strings.Contains(rep.Screen, "● Done: add the tests") {
		t.Fatalf("after send: %+v", rep)
	}

	// The agent's own terminal reads as its screen by default.
	r = command(caller, "terminal", "read", row.ID)
	if !strings.Contains(r.Text+string(r.Result), "Done: add the tests") {
		t.Errorf("terminal read: %+v", r)
	}

	r = command(caller, "agents", "list")
	var rows []AgentRow
	json.Unmarshal(r.Result, &rows)
	if len(rows) != 1 || rows[0].ID != row.ID {
		t.Errorf("list: %+v", rows)
	}
}

func TestAgentsWaitStopsOnAQuestion(t *testing.T) {
	_, caller := fakeAgents(t)
	row := startAgent(t, caller, "ask before editing")
	r := command(caller, "agents", "wait", row.ID, "--timeout", "20")
	var rep AgentReport
	json.Unmarshal(r.Result, &rep)
	if r.Exit != 2 || rep.Status != "attention" || !strings.Contains(r.Text, "Allow the edit?") {
		t.Fatalf("attention: exit %d %+v", r.Exit, rep)
	}
	// Answered, the bell is seen: the next wait is for the agent's work.
	if r := command(caller, "agents", "send", row.ID, "yes"); r.Error != "" {
		t.Fatal(r.Error)
	}
	r = command(caller, "agents", "wait", row.ID, "--timeout", "20")
	json.Unmarshal(r.Result, &rep)
	if r.Exit != 0 || rep.Status != "idle" || !strings.Contains(rep.Screen, "Done: yes") {
		t.Fatalf("after the answer: exit %d %+v", r.Exit, rep)
	}
}

func TestAgentsWaitSeesAQuestionWithoutBell(t *testing.T) {
	_, caller := fakeAgents(t)
	row := startAgent(t, caller, "trust prompt")
	r := command(caller, "agents", "wait", row.ID, "--timeout", "20")
	var rep AgentReport
	json.Unmarshal(r.Result, &rep)
	if r.Exit != 2 || rep.Status != "attention" || !strings.Contains(rep.Screen, "Do you trust") {
		t.Fatalf("trust prompt: exit %d %+v", r.Exit, rep)
	}
	// Answered, the question scrolls up: done again.
	if r := command(caller, "terminal", "write", row.ID, "go on", "--enter"); r.Error != "" {
		t.Fatal(r.Error)
	}
	r = command(caller, "agents", "wait", row.ID, "--timeout", "20")
	json.Unmarshal(r.Result, &rep)
	if r.Exit != 0 || !strings.Contains(rep.Screen, "Done: go on") {
		t.Fatalf("after the answer: exit %d %+v", r.Exit, rep)
	}
}

func TestAsks(t *testing.T) {
	for screen, want := range map[string]bool{
		"> Yes, I trust this folder\n  No, exit\n\n  ↑/↓ Navigate · enter Confirm": true,
		"❯ 1. Yes\n  2. No\n\nEnter to confirm · Esc to cancel":                    true,
		"Overwrite config? (y/n)":                                     true,
		"● Done: fixed it\n\n>\n? for shortcuts":                      false,
		"● Done: the middleware can now allow the caller through.\n>": false,
	} {
		if got := asks(screen); got != want {
			t.Errorf("asks(%q) = %v", screen, got)
		}
	}
}

func TestAgentsStartedByAgentsHaveALimit(t *testing.T) {
	main, caller := fakeAgents(t)
	a := startAgent(t, caller, "slow lead")
	b := startAgent(t, api.Caller{Session: a.ID, Cwd: main}, "helper")
	if b.Parent != a.ID {
		t.Errorf("parent: %+v", b)
	}
	r := command(api.Caller{Session: b.ID, Cwd: main}, "agents", "start", "helper of a helper")
	if !strings.Contains(r.Error, "levels deep") {
		t.Errorf("third level: %+v", r)
	}
	// specs revise can start an Elephant: the same limit.
	if r := command(api.Caller{Session: b.ID, Cwd: main}, "specs", "revise", "any-spec"); !strings.Contains(r.Error, "levels deep") {
		t.Errorf("specs revise at the third level: %+v", r)
	}
	if r := command(api.Caller{Session: a.ID, Cwd: main}, "agents", "wait", a.ID); !strings.Contains(r.Error, "itself") {
		t.Errorf("wait on itself: %+v", r)
	}
	r = command(caller, "agents", "wait", a.ID, "--timeout", "1")
	if r.Exit != 1 || !strings.Contains(r.Text, "still") {
		t.Errorf("timeout: %+v", r)
	}
	if r := command(caller, "agents", "send", "nope", "x"); r.Error == "" {
		t.Error("sent to an unknown agent")
	}
}

func TestAgentsWaitForSeveral(t *testing.T) {
	_, caller := fakeAgents(t)
	quick := startAgent(t, caller, "fix the login")
	slow := startAgent(t, caller, "slow refactor")
	both := quick.ID + "," + slow.ID

	// --any: back with the first that is done, the other still working.
	r := command(caller, "agents", "wait", both, "--any", "--timeout", "30")
	var reps AgentReports
	json.Unmarshal(r.Result, &reps)
	if r.Error != "" || r.Exit != 0 || len(reps.Agents) != 2 || reps.Waiting != 1 {
		t.Fatalf("--any: %+v", r)
	}
	if !strings.Contains(reps.Agents[0].Screen, "Done: fix the login") || reps.Agents[1].Screen != "" || reps.Agents[1].Status != "working" {
		t.Fatalf("--any reports: %+v", reps)
	}

	// All of them: a short timeout ends it with the slow one still at work.
	r = command(caller, "agents", "wait", both, "--timeout", "1")
	json.Unmarshal(r.Result, &reps)
	if r.Exit != 1 || reps.Waiting != 1 || !reps.Agents[1].TimedOut {
		t.Fatalf("timeout: exit %d %+v", r.Exit, reps)
	}

	// A question holds up the rest, so it ends the wait for all of them.
	asking := startAgent(t, caller, "ask before editing")
	r = command(caller, "agents", "wait", slow.ID+","+asking.ID, "--timeout", "30")
	json.Unmarshal(r.Result, &reps)
	if r.Exit != 2 || reps.Agents[1].Status != "attention" || !strings.Contains(r.Text, "Allow the edit?") {
		t.Fatalf("attention: exit %d %+v", r.Exit, reps)
	}

	if r := command(caller, "agents", "wait", quick.ID+","+quick.ID[:6]); !strings.Contains(r.Error, "twice") {
		t.Errorf("same agent twice: %+v", r)
	}
}
