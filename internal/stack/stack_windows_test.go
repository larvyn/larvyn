//go:build windows

package stack

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"larvyn/internal/proc"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// processes maps every running process id to "parent creationTime", so a
// process can be told apart from a later one that reuses its id.
func processes(t *testing.T) map[int][2]string {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"Get-CimInstance Win32_Process | ForEach-Object { \"$($_.ProcessId) $($_.ParentProcessId) $($_.CreationDate.Ticks)\" }").Output()
	if err != nil {
		t.Fatal(err)
	}
	all := map[int][2]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		all[pid] = [2]string{f[1], f[2]}
	}
	return all
}

// processTree returns root and every process below it, with creation times.
func processTree(t *testing.T, root int) map[int]string {
	all := processes(t)
	tree := map[int]string{root: all[root][1]}
	queue := []int{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for pid, info := range all {
			if info[0] == strconv.Itoa(cur) && tree[pid] == "" {
				tree[pid] = info[1]
				queue = append(queue, pid)
			}
		}
	}
	return tree
}

func alive(t *testing.T, tree map[int]string) []int {
	all := processes(t)
	var left []int
	for pid, created := range tree {
		if info, ok := all[pid]; ok && info[1] == created {
			left = append(left, pid)
		}
	}
	return left
}

// LARVYN_TEST_PROJECT=C:\Sites\acme-store go test -run StackStop -v
func TestStackStopKillsEveryProcess(t *testing.T) {
	dir := os.Getenv("LARVYN_TEST_PROJECT")
	if dir == "" {
		t.Skip("set LARVYN_TEST_PROJECT to a Laravel app")
	}
	p := store.Project{ID: "stacktest", Name: "stack test", Path: dir, Port: 8765}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) })
	defer store.Update(func(d *store.State) { d.Projects = d.Projects[:len(d.Projects)-1] })

	for _, it := range stackItems(p) {
		t.Logf("item %-8s dev=%v avail=%v %q", it.Key, it.Dev, it.Available, strings.Join(it.Argv, " "))
	}
	svc := &StackService{}
	keys := []string{"serve", "vite", "queue", "schedule"}
	if k := os.Getenv("LARVYN_TEST_KEYS"); k != "" {
		keys = strings.Split(k, ",")
	}
	for _, key := range keys {
		if _, err := svc.Start(p.ID, key); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	time.Sleep(10 * time.Second)
	for _, it := range stackItems(p) {
		if it.Session != nil {
			snap, _ := terminal.Sessions.Output(it.Session.ID)
			if len(snap) > 600 {
				snap = snap[len(snap)-600:]
			}
			t.Logf("%s running=%v exit=%d\n%s", it.Key, it.Session.Running, it.Session.ExitCode, proc.StripANSI(snap))
		}
	}

	trees := map[string]map[int]string{}
	for _, it := range stackItems(p) {
		if it.Session != nil && it.Session.Running {
			pid, _ := terminal.PID(it.Session.ID)
			trees[it.Key] = processTree(t, pid)
			t.Logf("%s: %d processes", it.Key, len(trees[it.Key]))
		}
	}
	svc.StopAll(p.ID)
	time.Sleep(3 * time.Second)
	for key, tree := range trees {
		if left := alive(t, tree); len(left) > 0 {
			t.Errorf("%s: still running after stop: %v", key, left)
		}
	}
}

// LARVYN_TEST_PROJECT=C:\Sites\acme-store go test -run CtrlC -v
func TestCtrlCStopsServe(t *testing.T) {
	dir := os.Getenv("LARVYN_TEST_PROJECT")
	if dir == "" {
		t.Skip("set LARVYN_TEST_PROJECT to a Laravel app")
	}
	p := store.Project{ID: "ctrlc", Name: "ctrl c", Path: dir, Port: 8767}
	store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) })
	defer store.Update(func(d *store.State) { d.Projects = d.Projects[:len(d.Projects)-1] })
	s, err := (&StackService{}).Start(p.ID, "serve")
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Sessions.Close(s.ID)
	time.Sleep(4 * time.Second)
	terminal.Sessions.Write(s.ID, "\x03")
	var info *terminal.SessionInfo
	for i := 0; i < 100; i++ {
		if info = terminal.Find(p.ID, "service", "serve"); info != nil && !info.Running {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	out, _ := terminal.Sessions.Output(s.ID)
	if info == nil || info.Running {
		t.Fatalf("serve still runs after Ctrl+C:\n%s", proc.StripANSI(out))
	}
	if strings.Contains(out, "Terminate batch job") {
		t.Errorf("cmd asked to terminate the batch job:\n%s", proc.StripANSI(out))
	}
	if !info.Stopped {
		t.Errorf("exit after Ctrl+C should count as stopped: %+v", info)
	}
}
