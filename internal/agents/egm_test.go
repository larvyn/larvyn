package agents

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"larvyn/internal/proc"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

func seedTestProject(t *testing.T) store.Project {
	t.Helper()
	t.Setenv("LARVYN_HOME", t.TempDir())
	store.Reload()
	p := store.Project{ID: store.NewID(), Name: "demo", Path: t.TempDir()}
	if err := store.Update(func(d *store.State) { d.Projects = append(d.Projects, p) }); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeAt writes a file under the project with a given modification time,
// so tests of "changed after its review" don't depend on the clock.
func writeAt(t *testing.T, root, rel, content string, at time.Time) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// sleepyAgent puts a fake agent on PATH that keeps running, so its session
// counts as working on the spec.
func sleepyAgent(t *testing.T, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix fake binary")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func closeAll(t *testing.T) {
	t.Cleanup(func() {
		for _, s := range terminal.Sessions.List() {
			terminal.Sessions.Close(s.ID)
		}
	})
}

func TestParseVerdictReadsOnlyTheVerdictLine(t *testing.T) {
	cases := []struct {
		md, want string
		allowed  []string
	}{
		{"Verdict: READY\n\nThe policy already blocks guests; nothing needs revision.", VerdictReady, reviewVerdicts},
		{"**Verdict:** NEEDS REVISION\n\nAlready good otherwise.", VerdictNeedsRevision, reviewVerdicts},
		{"## Verdict: blocked\n", VerdictBlocked, reviewVerdicts},
		{"# Review\n\nVerdict: `NEEDS_REVISION`", VerdictNeedsRevision, reviewVerdicts},
		{"Verdict: READY | NEEDS REVISION | BLOCKED\n\nVerdict: READY", VerdictReady, reviewVerdicts},
		{"The spec is ready to go, users are not blocked.", "", reviewVerdicts},
		{"Verdict: maybe", "", reviewVerdicts},
		{"Verdict: MINOR DRIFT\n\nMisaligned naming in one place.", VerdictMinorDrift, driftVerdicts},
		{"verdict: major-drift", VerdictMajorDrift, driftVerdicts},
		{"Verdict: ALIGNED.", VerdictAligned, driftVerdicts},
		{"Verdict: READY", "", driftVerdicts},
	}
	for _, c := range cases {
		if got := parseVerdict(c.md, c.allowed); got != c.want {
			t.Errorf("parseVerdict(%q) = %q, want %q", c.md, got, c.want)
		}
	}
}

func TestSlugsCutBetweenWords(t *testing.T) {
	for in, want := range map[string]string{
		"Let admins export the filtered orders list as a CSV download": "let-admins-export-the-filtered-orders",
		"Order Refunds":      "order-refunds",
		"¡Exportar pedidos!": "exportar-pedidos",
	} {
		if got, err := slugFor(in); err != nil || got != want {
			t.Errorf("slugFor(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

// A Goldfish whose TUI stays open after writing its review is done: it no
// longer blocks a revision or another review.
func TestAGoldfishThatReportedIsDone(t *testing.T) {
	sleepyAgent(t, "claude")
	closeAll(t)
	p := seedTestProject(t)
	svc := &AgentService{}
	writeAt(t, p.Path, specRelPath("export"), "# Export\n", time.Now().Add(-time.Hour))
	gf, err := svc.LaunchGoldfish(GoldfishRequest{ProjectID: p.ID, Agent: "claude", Slug: "export"})
	if err != nil {
		t.Fatal(err)
	}
	if len(runningEGM(p.ID, "export", "goldfish")) != 1 {
		t.Fatal("the Goldfish should be at work before it wrote its review")
	}
	writeAt(t, p.Path, reviewRelPath("export"), "Verdict: NEEDS REVISION\n", time.UnixMilli(gf.StartedAt).Add(time.Second))
	time.Sleep(WorkingFor + 200*time.Millisecond) // and went quiet
	if len(runningEGM(p.ID, "export", "goldfish")) != 0 {
		t.Fatal("a Goldfish that wrote its review and went quiet still counts as reviewing")
	}
	if _, err := svc.ReviseSpec(p.ID, "export", "claude"); err != nil {
		t.Fatalf("revise after the review: %v", err)
	}
}

func TestSpecLifecycle(t *testing.T) {
	p := seedTestProject(t)
	svc := &AgentService{}

	sf, err := svc.CreateSpec(p.ID, "Order Refunds")
	if err != nil {
		t.Fatal(err)
	}
	if sf.Slug != "order-refunds" || sf.Title != "Order Refunds" || !strings.Contains(sf.Content, "## Verification & Tests") {
		t.Fatalf("unexpected new spec: %+v", sf)
	}
	// A second spec of the same name gets its own file.
	again, err := svc.CreateSpec(p.ID, "Order refunds")
	if err != nil || again.Slug != "order-refunds-2" {
		t.Fatalf("got %q (%v), want order-refunds-2", again.Slug, err)
	}

	// Saving checks the editor's copy is still the one on disk.
	if _, err := svc.SaveSpec(p.ID, sf.Slug, "# Refunds\n\nPartial and full refunds.\n", sf.UpdatedAt-1000); !errors.Is(err, ErrSpecChanged) {
		t.Fatalf("got %v, want ErrSpecChanged", err)
	}
	saved, err := svc.SaveSpec(p.ID, sf.Slug, "# Refunds\n\nPartial and full refunds.\n", sf.UpdatedAt)
	if err != nil || saved.Title != "Refunds" || saved.Summary != "Partial and full refunds." {
		t.Fatalf("unexpected save: %+v (%v)", saved, err)
	}

	list, err := svc.Specs(p.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("got %d specs (%v), want 2", len(list), err)
	}

	if err := svc.DeleteSpec(p.ID, sf.Slug); err != nil {
		t.Fatal(err)
	}
	if list, _ = svc.Specs(p.ID); len(list) != 1 || list[0].Slug != "order-refunds-2" {
		t.Fatalf("unexpected specs after delete: %+v", list)
	}
}

func TestReviewApprovalAndStaleness(t *testing.T) {
	p := seedTestProject(t)
	svc := &AgentService{}
	t0 := time.Now().Add(-time.Hour)
	writeAt(t, p.Path, specRelPath("billing"), "# Billing\n", t0)

	sf, _ := svc.ReadSpec(p.ID, "billing")
	if sf.HasReview || sf.Approved || sf.Rounds != 0 {
		t.Fatalf("unreviewed spec: %+v", sf)
	}

	writeAt(t, p.Path, reviewRelPath("billing"), "Verdict: BLOCKED\n\nWho owns a seat?", t0.Add(time.Minute))
	sf, _ = svc.ReadSpec(p.ID, "billing")
	if sf.Verdict != VerdictBlocked || sf.Approved || sf.Stale || sf.Rounds != 1 {
		t.Fatalf("blocked review: %+v", sf)
	}

	// The Elephant (or the user) changes the spec after a BLOCKED review:
	// that is not an approval, the spec needs another review.
	writeAt(t, p.Path, specRelPath("billing"), "# Billing\n\nTeams own seats.\n", t0.Add(2*time.Minute))
	sf, _ = svc.ReadSpec(p.ID, "billing")
	if sf.Verdict != VerdictBlocked || !sf.Stale || sf.Approved {
		t.Fatalf("revised spec: %+v", sf)
	}

	writeAt(t, p.Path, reviewRelPath("billing"), "Verdict: READY\n", t0.Add(3*time.Minute))
	sf, _ = svc.ReadSpec(p.ID, "billing")
	if !sf.Approved {
		t.Fatalf("ready review of the current spec: %+v", sf)
	}

	// Builders need an approved spec, or Force.
	writeAt(t, p.Path, specRelPath("billing"), "# Billing\n\nTeams own seats, admins too.\n", t0.Add(4*time.Minute))
	_, err := svc.LaunchFromSpec(SpecLaunchRequest{ProjectID: p.ID, Slug: "billing"}, []string{"claude"})
	if err == nil || !strings.Contains(err.Error(), "changed after its review") {
		t.Fatalf("got %v, want a stale-review error", err)
	}
}

func TestGoldfishArchivesThePreviousReview(t *testing.T) {
	sleepyAgent(t, "claude")
	closeAll(t)
	p := seedTestProject(t)
	svc := &AgentService{}
	writeAt(t, p.Path, specRelPath("export"), "# CSV export\n", time.Now().Add(-time.Hour))
	writeAt(t, p.Path, reviewRelPath("export"), "Verdict: NEEDS REVISION\n", time.Now().Add(-30*time.Minute))

	gf, err := svc.LaunchGoldfish(GoldfishRequest{ProjectID: p.ID, Agent: "claude", Slug: "export"})
	if err != nil {
		t.Fatal(err)
	}
	if gf.Meta["role"] != "goldfish" || gf.Meta["stateless"] != "1" || !strings.HasPrefix(gf.Title, "Review 2 ·") {
		t.Fatalf("unexpected goldfish: %q %+v", gf.Title, gf.Meta)
	}
	if _, err := os.Stat(filepath.Join(p.Path, ".larvyn", "specs", "history", "export.review.1.md")); err != nil {
		t.Fatalf("previous review not archived: %v", err)
	}
	sf, _ := svc.ReadSpec(p.ID, "export")
	if sf.HasReview || sf.Rounds != 1 {
		t.Fatalf("the old review must not show as the new one: %+v", sf)
	}
	// One Goldfish per spec at a time.
	if _, err := svc.LaunchGoldfish(GoldfishRequest{ProjectID: p.ID, Agent: "claude", Slug: "export"}); err == nil {
		t.Fatal("a second Goldfish started on the same spec")
	}
	// No revision while it reviews.
	if _, err := svc.ReviseSpec(p.ID, "export", "claude"); err == nil {
		t.Fatal("revised a spec that has no review yet")
	}
}

func TestElephantNewSpecsNeverOverwriteOldOnes(t *testing.T) {
	sleepyAgent(t, "claude")
	closeAll(t)
	p := seedTestProject(t)
	svc := &AgentService{}
	writeAt(t, p.Path, specRelPath("add-two-factor-authentication"), "# Mine\n\nKeep me.\n", time.Now())

	el, err := svc.LaunchElephant(ElephantRequest{ProjectID: p.ID, Agent: "claude", Prompt: "Add two-factor authentication"})
	if err != nil {
		t.Fatal(err)
	}
	if el.Meta["spec"] != "add-two-factor-authentication-2" || el.Meta["role"] != "elephant" {
		t.Fatalf("unexpected elephant meta: %+v", el.Meta)
	}
	if b, _ := os.ReadFile(filepath.Join(p.Path, ".larvyn", "specs", "add-two-factor-authentication.md")); !strings.Contains(string(b), "Keep me") {
		t.Fatal("the existing spec was overwritten")
	}
	sf, err := svc.ReadSpec(p.ID, el.Meta["spec"])
	if err != nil || !strings.Contains(sf.Content, "Add two-factor authentication") {
		t.Fatalf("no skeleton for the new spec: %+v (%v)", sf, err)
	}
	if _, err := svc.LaunchElephant(ElephantRequest{ProjectID: p.ID, Agent: "claude", Slug: el.Meta["spec"]}); err == nil {
		t.Fatal("two Elephants on one spec")
	}
	if err := svc.DeleteSpec(p.ID, el.Meta["spec"]); err == nil {
		t.Fatal("deleted a spec an Elephant is writing")
	}
}

func TestReviseSpecTalksToTheRunningElephant(t *testing.T) {
	sleepyAgent(t, "claude")
	closeAll(t)
	p := seedTestProject(t)
	svc := &AgentService{}
	el, err := svc.LaunchElephant(ElephantRequest{ProjectID: p.ID, Agent: "claude", Prompt: "Team invitations"})
	if err != nil {
		t.Fatal(err)
	}
	writeAt(t, p.Path, reviewRelPath(el.Meta["spec"]), "Verdict: NEEDS REVISION\n", time.Now().Add(time.Second))
	got, err := svc.ReviseSpec(p.ID, el.Meta["spec"], "claude")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != el.ID {
		t.Fatalf("started a new Elephant (%s) instead of using %s", got.ID, el.ID)
	}
}

func TestStatelessSessionsDoNotResume(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix fake binary")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, resumes, err := RelaunchArgv(terminal.SessionSpec{Key: "opencode", Meta: map[string]string{}}, false); err != nil || !resumes {
		t.Fatalf("a normal session resumes, got resumes=%v err=%v", resumes, err)
	}
	argv, resumes, err := RelaunchArgv(terminal.SessionSpec{Key: "opencode", Meta: map[string]string{"role": "goldfish", "stateless": "1"}}, false)
	if err != nil || resumes || len(argv) != 1 {
		t.Fatalf("a Goldfish starts cold, got resumes=%v argv=%q err=%v", resumes, argv, err)
	}
}

func TestBuildersAndDriftCheck(t *testing.T) {
	sleepyAgent(t, "claude")
	closeAll(t)
	p := seedTestProject(t)
	svc := &AgentService{}
	wt := t.TempDir()
	prevCreate, prevRemove := CreateWorktree, RemoveWorktree
	CreateWorktree = func(projectID, name, base string) (string, string, error) { return wt, "larvyn/" + name, nil }
	removed := ""
	RemoveWorktree = func(projectID, path string) error { removed = path; return nil }
	t.Cleanup(func() { CreateWorktree, RemoveWorktree = prevCreate, prevRemove })

	t0 := time.Now().Add(-time.Hour)
	writeAt(t, p.Path, specRelPath("two-factor"), "# Two-factor\n\nTOTP with recovery codes.\n", t0)
	writeAt(t, p.Path, reviewRelPath("two-factor"), "Verdict: READY\n\nRate limit OTP checks.", t0.Add(time.Minute))

	builders, err := svc.LaunchFromSpec(SpecLaunchRequest{ProjectID: p.ID, Slug: "two-factor", Worktree: true}, []string{"claude"})
	if err != nil || len(builders) != 1 {
		t.Fatalf("got %v (%v), want 1 builder", builders, err)
	}
	b := builders[0]
	if b.Meta["role"] != "builder" || b.Meta["spec"] != "two-factor" || b.Meta["worktree"] != wt {
		t.Fatalf("unexpected builder meta: %+v", b.Meta)
	}
	for _, rel := range []string{specRelPath("two-factor"), reviewRelPath("two-factor")} {
		if _, err := os.Stat(filepath.Join(wt, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s not copied into the worktree: %v", rel, err)
		}
	}

	// Only the project and its worktrees can be audited.
	if _, err := svc.LaunchDriftCheck(DriftRequest{ProjectID: p.ID, Agent: "claude", Slug: "two-factor", Worktree: t.TempDir()}); err == nil {
		t.Fatal("audited a folder outside the project")
	}
	if err := svc.FixDrift(b.ID); err == nil {
		t.Fatal("sent fixes without a drift report")
	}

	dr, err := svc.LaunchDriftCheck(DriftRequest{ProjectID: p.ID, Agent: "claude", Slug: "two-factor", Worktree: wt})
	if err != nil {
		t.Fatal(err)
	}
	if dr.Meta["role"] != "drift" || dr.Meta["stateless"] != "1" || !samePath(dr.Cwd, wt) {
		t.Fatalf("unexpected drift session: %+v", dr)
	}
	if _, err := svc.LaunchDriftCheck(DriftRequest{ProjectID: p.ID, Agent: "claude", Slug: "two-factor", Worktree: wt}); err == nil {
		t.Fatal("two drift checks in one worktree")
	}
	writeAt(t, wt, driftRelPath("two-factor"), "Verdict: MINOR DRIFT\n\n- [ ] recovery codes", time.Now())
	rep, err := svc.ReadDrift(p.ID, wt, "two-factor")
	if err != nil || rep.Verdict != VerdictMinorDrift || rep.Stale {
		t.Fatalf("unexpected report: %+v (%v)", rep, err)
	}
	if err := svc.FixDrift(b.ID); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("got %v, want an error while the drift check runs", err)
	}
	terminal.Sessions.Close(dr.ID)
	if err := svc.FixDrift(b.ID); err != nil {
		t.Fatalf("sending the fixes: %v", err)
	}

	// A worktree hook that fails takes the worktree back out.
	if err := os.WriteFile(filepath.Join(wt, ".larvyn", "specs", "two-factor.md"), nil, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(wt, ".larvyn", "specs"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(wt, ".larvyn", "specs"), 0o755) })
	if os.Getuid() != 0 {
		if _, err := svc.LaunchFromSpec(SpecLaunchRequest{ProjectID: p.ID, Slug: "two-factor", Worktree: true}, []string{"claude"}); err == nil || removed != wt {
			t.Fatalf("got err=%v removed=%q: a builder without its spec must not start", err, removed)
		}
	}
}

func TestExcludeSpecsKeepsThemOutOfGit(t *testing.T) {
	dir := t.TempDir()
	if _, err := proc.Git(dir, "init", "-q"); err != nil {
		t.Skip("git not available")
	}
	excludeSpecs(dir)
	excludeSpecs(dir) // once only
	b, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "/.larvyn/specs/"); n != 1 {
		t.Fatalf("exclude has the specs line %d times:\n%s", n, b)
	}
	writeAt(t, dir, specRelPath("x"), "# X\n", time.Now())
	writeAt(t, dir, ".larvyn/project.json", "{}", time.Now())
	out, _ := proc.Git(dir, "status", "--porcelain", "--untracked-files=all")
	if strings.Contains(out, "specs") || !strings.Contains(out, ".larvyn/project.json") {
		t.Fatalf("specs must be ignored and the project file not:\n%s", out)
	}
}
