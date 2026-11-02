package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"larvyn/internal/terminal"
)

func TestClaudeRelaunchResumesItsConversation(t *testing.T) {
	bin := fakeBinary(t, "claude")
	t.Setenv("PATH", filepath.Dir(bin))
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	spec := terminal.SessionSpec{Kind: "agent", Key: "claude", Meta: map[string]string{"resumeId": "abc", "yolo": "1"}}

	argv, resumes, err := RelaunchArgv(spec, false)
	if err != nil || !resumes {
		t.Fatal(err, resumes)
	}
	// Nothing was recorded yet: start under the same id, no prompt.
	if want := []string{"--dangerously-skip-permissions", "--session-id", "abc"}; !reflect.DeepEqual(argv[1:], want) {
		t.Fatalf("got %v, want %v", argv[1:], want)
	}

	dir := filepath.Join(cfg, "projects", "-tmp-app")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "abc.jsonl"), []byte("{}\n"), 0o644)
	argv, _, _ = RelaunchArgv(spec, false)
	if want := []string{"--dangerously-skip-permissions", "--resume", "abc"}; !reflect.DeepEqual(argv[1:], want) {
		t.Fatalf("got %v, want %v", argv[1:], want)
	}
}

func TestAgentWithoutResumeStartsAfresh(t *testing.T) {
	bin := fakeBinary(t, "cursor-agent")
	t.Setenv("PATH", filepath.Dir(bin))
	argv, resumes, err := RelaunchArgv(terminal.SessionSpec{Kind: "agent", Key: "cursor-agent", Meta: map[string]string{"prompt": "fix it"}}, false)
	if err != nil || resumes || len(argv) != 1 {
		t.Fatalf("argv %v resumes %v err %v", argv, resumes, err)
	}
}

func TestCodexSessionIDMatchesFolderAndTime(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	started := time.Now().Add(-time.Minute)
	day := filepath.Join(root, "sessions", started.Format("2006"), started.Format("01"), started.Format("02"))
	_ = os.MkdirAll(day, 0o755)
	write := func(name, id, cwd string, at time.Time) {
		line := `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `","timestamp":"` + at.UTC().Format(time.RFC3339Nano) + `"}}` + "\n"
		_ = os.WriteFile(filepath.Join(day, "rollout-"+name+".jsonl"), []byte(line), 0o644)
	}
	write("1", "older", "/app", started.Add(-time.Hour))
	write("2", "other-dir", "/elsewhere", started.Add(time.Second))
	write("3", "mine", "/app", started.Add(2*time.Second))
	write("4", "later", "/app", started.Add(10*time.Second))

	if got := codexSessionID("/app", started.UnixMilli(), map[string]bool{}); got != "mine" {
		t.Fatalf("got %q, want mine", got)
	}
	if got := codexSessionID("/app", started.UnixMilli(), map[string]bool{"mine": true}); got != "later" {
		t.Fatalf("a claimed conversation must be skipped, got %q", got)
	}
}

func fakeBinary(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// Of several sessions in one folder, an agent that resumes "the latest
// conversation here" resumes only in the one that talked last; Claude and
// Codex resume by id and do not care.
func TestSharedFolderStopsResumeByFolder(t *testing.T) {
	oc, cl := fakeBinary(t, "opencode"), fakeBinary(t, "claude")
	t.Setenv("PATH", filepath.Dir(oc)+string(os.PathListSeparator)+filepath.Dir(cl))
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	argv, resumes, err := RelaunchArgv(terminal.SessionSpec{Key: "opencode"}, true)
	if err != nil || resumes || len(argv) != 1 {
		t.Fatalf("shared opencode: argv %v resumes %v err %v", argv, resumes, err)
	}
	if _, resumes, _ := RelaunchArgv(terminal.SessionSpec{Key: "opencode"}, false); !resumes {
		t.Fatal("opencode alone in its folder resumes")
	}
	if _, resumes, _ := RelaunchArgv(terminal.SessionSpec{Key: "claude", Meta: map[string]string{"resumeId": "x"}}, true); !resumes {
		t.Fatal("claude resumes by id, shared or not")
	}
}
