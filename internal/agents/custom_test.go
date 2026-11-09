package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

func setCustom(t *testing.T, list ...store.CustomAgent) {
	t.Helper()
	set := func(l []store.CustomAgent) {
		if err := store.Update(func(d *store.State) { d.Settings.CustomAgents = l }); err != nil {
			t.Fatal(err)
		}
	}
	set(list)
	t.Cleanup(func() { set(nil) })
}

// A custom agent works like its base: Claude's flags and session id, its own
// command and environment, and it resumes from its own config folder.
func TestCustomAgentWorksLikeItsBase(t *testing.T) {
	useYolo(t)
	bin := fakeBinary(t, "claude-os")
	home, _ := os.UserHomeDir()
	cfg := t.TempDir()
	setCustom(t, store.CustomAgent{ID: "custom-os", Name: "Claude OS", Base: "claude", Command: bin, Env: map[string]string{"CLAUDE_CONFIG_DIR": cfg, "NOTE": "~/x"}})

	k := kindByID(kinds(), "custom-os")
	if k == nil || k.Base != "claude" || k.Binary != bin || !k.PromptAsArg || k.env["NOTE"] != filepath.Join(home, "x") {
		t.Fatalf("got %+v", k)
	}

	spec := terminal.SessionSpec{Kind: "agent", Key: "custom-os", Env: k.env, Meta: map[string]string{"resumeId": "abc", "yolo": "1"}}
	argv, resumes, err := RelaunchArgv(spec, false)
	if want := []string{bin, "--dangerously-skip-permissions", "--session-id", "abc"}; err != nil || !resumes || !reflect.DeepEqual(argv, want) {
		t.Fatalf("got %v %v %v, want %v", argv, resumes, err, want)
	}
	// The conversation is in the agent's config folder, not Larvyn's.
	dir := filepath.Join(cfg, "projects", "-app")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "abc.jsonl"), []byte("{}\n"), 0o644)
	argv, _, _ = RelaunchArgv(spec, false)
	if want := []string{bin, "--dangerously-skip-permissions", "--resume", "abc"}; !reflect.DeepEqual(argv, want) {
		t.Fatalf("got %v, want %v", argv, want)
	}
}

// A custom agent removed and added again gets a new id; its sessions still
// relaunch, and resume, through the agent with their name.
func TestReaddedCustomAgentStillResumes(t *testing.T) {
	bin := fakeBinary(t, "claude-os")
	setCustom(t, store.CustomAgent{ID: "custom-new", Name: "Claude OS", Base: "claude", Command: bin})
	spec := terminal.SessionSpec{Kind: "agent", Key: "custom-old", Meta: map[string]string{"agentName": "Claude OS", "resumeId": "abc"}}
	argv, resumes, err := RelaunchArgv(spec, false)
	if want := []string{bin, "--session-id", "abc"}; err != nil || !resumes || !reflect.DeepEqual(argv, want) {
		t.Fatalf("got %v %v %v, want %v", argv, resumes, err, want)
	}
	spec.Meta["agentName"] = "Someone else"
	if _, _, err := RelaunchArgv(spec, false); err == nil {
		t.Fatal("a session of an agent that is gone must not relaunch")
	}
}

// Custom agents can't replace a built-in one, and incomplete ones are left
// out rather than offered.
func TestCustomAgentsThatAreLeftOut(t *testing.T) {
	setCustom(t,
		store.CustomAgent{ID: "claude", Base: "codex", Command: "x"},
		store.CustomAgent{ID: "a", Base: "nope", Command: "x"},
		store.CustomAgent{ID: "b", Base: "claude"},
		store.CustomAgent{ID: "", Base: "claude", Command: "x"},
		store.CustomAgent{ID: "c", Base: "codex", Command: "x"},
		store.CustomAgent{ID: "c", Base: "claude", Command: "y"},
	)
	all := kinds()
	if len(all) != len(agentKinds)+1 || all[len(all)-1].ID != "c" || all[len(all)-1].Base != "codex" || all[len(all)-1].Name != "x" {
		t.Fatalf("got %+v", all[len(agentKinds):])
	}
	if k := kindByID(all, "claude"); k.Custom || k.Binary != "claude" {
		t.Fatalf("the built-in claude was replaced: %+v", k)
	}
}

// Launching a custom agent runs its command with its environment and its
// base's arguments.
func TestLaunchCustomAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	useYolo(t)
	dir, out := t.TempDir(), filepath.Join(t.TempDir(), "out")
	bin := filepath.Join(dir, "claude-os")
	_ = os.WriteFile(bin, []byte("#!/bin/sh\necho \"$CLAUDE_CONFIG_DIR $*\" > "+out+"\n"), 0o755)
	setCustom(t, store.CustomAgent{ID: "custom-os", Name: "Claude OS", Base: "claude", Command: bin, Env: map[string]string{"CLAUDE_CONFIG_DIR": "/cfg"}})
	_ = store.Update(func(d *store.State) {
		d.Projects = append(d.Projects, store.Project{ID: "p-custom", Name: "app", Path: dir})
	})

	s, err := (&AgentService{}).Launch(LaunchRequest{ProjectID: "p-custom", Agent: "custom-os", Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Sessions.Close(s.ID)
	var got []byte
	for i := 0; i < 50 && len(got) == 0; i++ {
		time.Sleep(100 * time.Millisecond)
		got, _ = os.ReadFile(out)
	}
	want := "/cfg --dangerously-skip-permissions --session-id " + s.Meta["resumeId"] + " hi\n"
	if string(got) != want || s.Key != "custom-os" || s.Meta["agentName"] != "Claude OS" {
		t.Fatalf("got %q (key %s), want %q", got, s.Key, want)
	}
}
