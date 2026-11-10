package agentapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"larvyn/internal/api"
	"larvyn/internal/terminal"
)

func TestLocks(t *testing.T) {
	main, wt := gitRepo(t)
	addProject(t, main)
	a, b := api.Caller{Cwd: main}, api.Caller{Cwd: wt}

	r := command(a, "lock", "acquire", "db", "--note", "migrating")
	var res LockResult
	json.Unmarshal(r.Result, &res)
	if r.Error != "" || r.Exit != 0 || !res.Acquired || res.Lock.Expires < 590 {
		t.Fatalf("acquire: %+v", r)
	}
	// Held by a: b gives up at once, or after its wait.
	r = command(b, "lock", "acquire", "db", "--nowait")
	if r.Exit != 1 || !strings.Contains(r.Text, "migrating") {
		t.Fatalf("held: %+v", r)
	}
	if r := command(b, "lock", "release", "db"); !strings.Contains(r.Error, "not by you") {
		t.Fatalf("release by another: %+v", r)
	}
	r = command(a, "lock", "list")
	var rows []LockRow
	json.Unmarshal(r.Result, &rows)
	if len(rows) != 1 || rows[0].Name != "db" || !rows[0].Mine {
		t.Fatalf("list: %+v", rows)
	}

	// b waits; a releases; b gets it.
	done := make(chan api.Response, 1)
	go func() { done <- command(b, "lock", "acquire", "db", "--wait", "10") }()
	time.Sleep(500 * time.Millisecond)
	if r := command(a, "lock", "release", "db"); r.Error != "" {
		t.Fatal(r.Error)
	}
	select {
	case r = <-done:
		if r.Exit != 0 {
			t.Fatalf("after release: %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("b never got the lock")
	}

	// A short lease runs out on its own.
	if r := command(a, "lock", "acquire", "main", "--ttl", "1"); r.Exit != 0 {
		t.Fatal(r)
	}
	time.Sleep(1100 * time.Millisecond)
	if r := command(b, "lock", "acquire", "main", "--nowait"); r.Exit != 0 {
		t.Fatalf("expired lease: %+v", r)
	}
}

func TestLockEndsWithItsTerminal(t *testing.T) {
	main, caller := fakeAgents(t)
	row := startAgent(t, caller, "fix the login")
	agent := api.Caller{Session: row.ID, Cwd: main}
	if r := command(agent, "lock", "acquire", "db"); r.Exit != 0 {
		t.Fatalf("agent acquire: %+v", r)
	}
	if r := command(caller, "lock", "acquire", "db", "--nowait"); r.Exit != 1 || !strings.Contains(r.Text, row.ID) {
		t.Fatalf("held by the agent: %+v", r)
	}
	if err := terminal.Sessions.Close(row.ID); err != nil {
		t.Fatal(err)
	}
	if r := command(caller, "lock", "acquire", "db", "--nowait"); r.Exit != 0 {
		t.Fatalf("after the agent's terminal closed: %+v", r)
	}
}

func TestLockOwners(t *testing.T) {
	main, _ := gitRepo(t)
	addProject(t, main)

	// A session that isn't an open terminal can't hold a lock: it would end
	// at once while the caller thinks it holds it.
	if r := command(api.Caller{Cwd: main, Session: "gone"}, "lock", "acquire", "db"); !strings.Contains(r.Error, "isn't open") {
		t.Fatalf("stale session: %+v", r)
	}

	// Outside Larvyn's terminals, callers in one folder are one owner unless
	// they name themselves.
	c := api.Caller{Cwd: main}
	if r := command(c, "lock", "acquire", "db", "--owner", "agent-a"); r.Exit != 0 {
		t.Fatalf("a: %+v", r)
	}
	if r := command(c, "lock", "acquire", "db", "--owner", "agent-b", "--nowait"); r.Exit != 1 || !strings.Contains(r.Text, "agent-a") {
		t.Fatalf("b while a holds it: %+v", r)
	}
	if r := command(c, "lock", "release", "db"); !strings.Contains(r.Error, "not by you") {
		t.Fatalf("the folder releasing a's lock: %+v", r)
	}
	if r := command(c, "lock", "release", "db", "--owner", "agent-a"); r.Error != "" {
		t.Fatalf("a releases: %+v", r)
	}
}
