package agents

// The Elephant-Goldfish Model (EGM): a spec-driven way to run agents.
//
//  1. The Elephant, a long-lived session with the whole conversation,
//     explores the codebase and writes .larvyn/specs/<slug>.md. It writes
//     no application code.
//  2. The Goldfish, a fresh session with no memory (never resumed), reads
//     the spec cold against the code and writes <slug>.review.md, whose
//     first line is its verdict. Its amendments go back to the Elephant,
//     and the revised spec is reviewed again, until a review says READY.
//  3. Builders implement the approved spec, one per worktree.
//  4. A Goldfish auditor compares each builder's changes with the spec and
//     writes <slug>.drift.md; its findings go back to the builder, and the
//     winning attempt is merged.
//
// The spec folder lives in the main checkout and is never committed: it is
// added to the repository's .git/info/exclude, MergeWorktree leaves it out,
// and each worktree gets a copy of the spec it builds.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"larvyn/internal/proc"
	"larvyn/internal/safefile"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

const (
	specsRelDir   = ".larvyn/specs"
	historyRelDir = ".larvyn/specs/history"
	maxSpecBytes  = 256 << 10
)

// The verdicts a Goldfish writes on the first line of a review or drift
// report.
const (
	VerdictReady         = "READY"
	VerdictNeedsRevision = "NEEDS REVISION"
	VerdictBlocked       = "BLOCKED"
	VerdictAligned       = "ALIGNED"
	VerdictMinorDrift    = "MINOR DRIFT"
	VerdictMajorDrift    = "MAJOR DRIFT"
)

var (
	reviewVerdicts = []string{VerdictNeedsRevision, VerdictBlocked, VerdictReady}
	driftVerdicts  = []string{VerdictMinorDrift, VerdictMajorDrift, VerdictAligned}
)

// ErrSpecChanged is SaveSpec's answer when the file changed on disk after
// the editor loaded it (the Elephant kept writing): saving would lose that.
var ErrSpecChanged = errors.New("the spec changed on disk since you opened it: reload it and apply your edits again")

var validSlugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

// SpecFile is a spec, .larvyn/specs/<slug>.md, with its latest Goldfish
// review, <slug>.review.md.
type SpecFile struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Path    string `json:"path"`    // relative to the project, with slashes
	AbsPath string `json:"absPath"` // for the editor
	Summary string `json:"summary"`
	Content string `json:"content"`
	Review  string `json:"review"`
	// Verdict is the review's: READY, NEEDS REVISION, BLOCKED, or "" when
	// there is no review or it names no verdict.
	Verdict   string `json:"verdict"`
	HasReview bool   `json:"hasReview"`
	// Stale is set when the spec changed after its review: the review no
	// longer vouches for it, so it has to be reviewed again.
	Stale bool `json:"stale"`
	// Approved is a READY review of the spec as it is now.
	Approved bool `json:"approved"`
	// Rounds counts the reviews so far, the archived ones included.
	Rounds    int   `json:"rounds"`
	UpdatedAt int64 `json:"updatedAt"`
	ReviewAt  int64 `json:"reviewAt"`
	Size      int64 `json:"size"`
}

// ElephantRequest starts an Elephant on a new spec (Slug empty) or on an
// existing one.
type ElephantRequest struct {
	ProjectID string `json:"projectId"`
	Agent     string `json:"agent"`
	Prompt    string `json:"prompt"`
	Slug      string `json:"slug"`
	// Meta is added to the session's meta (the local API marks who started
	// it); not part of the frontend's bindings.
	Meta map[string]string `json:"-"`
}

// GoldfishRequest starts a cold review of a spec.
type GoldfishRequest struct {
	ProjectID string            `json:"projectId"`
	Agent     string            `json:"agent"`
	Slug      string            `json:"slug"`
	Focus     string            `json:"focus"`
	Meta      map[string]string `json:"-"`
}

// SpecLaunchRequest hands a spec to builders.
type SpecLaunchRequest struct {
	ProjectID   string `json:"projectId"`
	Slug        string `json:"slug"`
	Notes       string `json:"notes"`
	Worktree    bool   `json:"worktree"`
	InstallDeps bool   `json:"installDeps"`
	Base        string `json:"base"`
	// Force builds a spec that has no READY review of its current version.
	Force bool              `json:"force"`
	Meta  map[string]string `json:"-"`
}

// DriftRequest audits a builder's folder against a spec.
type DriftRequest struct {
	ProjectID string `json:"projectId"`
	Agent     string `json:"agent"`
	Slug      string `json:"slug"`
	// Worktree is the folder the builder works in: one of the project's
	// worktrees, or the project itself.
	Worktree string            `json:"worktree"`
	Meta     map[string]string `json:"-"`
}

// DriftReport is <slug>.drift.md in a builder's folder.
type DriftReport struct {
	Slug      string `json:"slug"`
	Worktree  string `json:"worktree"`
	Verdict   string `json:"verdict"`
	Content   string `json:"content"`
	UpdatedAt int64  `json:"updatedAt"`
	// Stale is set when the spec changed after the audit.
	Stale bool `json:"stale"`
}

func normalizeSpecSlug(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	for _, suf := range []string{".review.md", ".drift.md", ".md"} {
		s = strings.TrimSuffix(s, suf)
	}
	if validSlugRe.MatchString(s) {
		return s, nil
	}
	if s = proc.Slug(s); !validSlugRe.MatchString(s) {
		return "", errors.New("invalid spec name")
	}
	return s, nil
}

func specRelPath(slug string) string   { return specsRelDir + "/" + slug + ".md" }
func reviewRelPath(slug string) string { return specsRelDir + "/" + slug + ".review.md" }
func driftRelPath(slug string) string  { return specsRelDir + "/" + slug + ".drift.md" }

// freeSlug is base, or base-2, base-3… when a spec already has it.
func freeSlug(root, base string) string {
	slug := base
	for n := 2; ; n++ {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(specRelPath(slug)))); errors.Is(err, os.ErrNotExist) {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", strings.TrimRight(base[:min(len(base), 60)], "-"), n)
	}
}

func project(id string) (store.Project, error) {
	p, ok := store.FindProject(id)
	if !ok {
		return p, errors.New("project not found")
	}
	return p, nil
}

// Specs lists the project's specs, the most recently changed first.
func (a *AgentService) Specs(projectID string) ([]SpecFile, error) {
	p, err := project(projectID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(p.Path, filepath.FromSlash(specsRelDir)))
	if errors.Is(err, os.ErrNotExist) {
		return []SpecFile{}, nil
	} else if err != nil {
		return nil, err
	}
	out := []SpecFile{}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".review.md") || strings.HasSuffix(name, ".drift.md") {
			continue
		}
		slug := strings.TrimSuffix(name, ".md")
		if !validSlugRe.MatchString(slug) {
			continue
		}
		if sf, err := readSpecIn(p.Path, slug, false); err == nil {
			out = append(out, sf)
		}
	}
	slices.SortFunc(out, func(a, b SpecFile) int {
		return cmp.Compare(max(b.UpdatedAt, b.ReviewAt), max(a.UpdatedAt, a.ReviewAt))
	})
	return out, nil
}

// ReadSpec reads a spec with its review.
func (a *AgentService) ReadSpec(projectID, slug string) (SpecFile, error) {
	p, err := project(projectID)
	if err != nil {
		return SpecFile{}, err
	}
	clean, err := normalizeSpecSlug(slug)
	if err != nil {
		return SpecFile{}, err
	}
	return readSpecIn(p.Path, clean, true)
}

// CreateSpec writes a new spec from the template, under a name no other
// spec has, for the user to fill in.
func (a *AgentService) CreateSpec(projectID, title string) (SpecFile, error) {
	p, err := project(projectID)
	if err != nil {
		return SpecFile{}, err
	}
	title = firstLine(cmp.Or(strings.TrimSpace(title), "New feature"), 72)
	base, err := slugFor(title)
	if err != nil {
		return SpecFile{}, err
	}
	slug := freeSlug(p.Path, base)
	excludeSpecs(p.Path)
	if err := writeSpecFile(p.Path, specRelPath(slug), []byte(specSkeleton(title, ""))); err != nil {
		return SpecFile{}, err
	}
	return readSpecIn(p.Path, slug, true)
}

// SaveSpec writes a spec the user edited. base is the UpdatedAt the editor
// loaded; when the file changed since, nothing is written and the answer
// is ErrSpecChanged. 0 skips the check.
func (a *AgentService) SaveSpec(projectID, slug, content string, base int64) (SpecFile, error) {
	p, err := project(projectID)
	if err != nil {
		return SpecFile{}, err
	}
	clean, err := normalizeSpecSlug(slug)
	if err != nil {
		return SpecFile{}, err
	}
	if len(content) > maxSpecBytes {
		return SpecFile{}, safefile.ErrTooLarge
	}
	if base > 0 {
		if cur, err := readSpecIn(p.Path, clean, false); err == nil && cur.UpdatedAt != base {
			return SpecFile{}, ErrSpecChanged
		}
	}
	excludeSpecs(p.Path)
	if err := writeSpecFile(p.Path, specRelPath(clean), []byte(content)); err != nil {
		return SpecFile{}, err
	}
	return readSpecIn(p.Path, clean, true)
}

// DeleteSpec removes a spec with its review, drift report and history.
func (a *AgentService) DeleteSpec(projectID, slug string) error {
	p, err := project(projectID)
	if err != nil {
		return err
	}
	clean, err := normalizeSpecSlug(slug)
	if err != nil {
		return err
	}
	if s := runningEGM(p.ID, clean, ""); len(s) > 0 {
		return fmt.Errorf("%s is still working on this spec: stop it first", s[0].Title)
	}
	root, err := os.OpenRoot(p.Path)
	if err != nil {
		return err
	}
	defer root.Close()
	rels := []string{specRelPath(clean), reviewRelPath(clean), driftRelPath(clean)}
	if entries, err := os.ReadDir(filepath.Join(p.Path, filepath.FromSlash(historyRelDir))); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), clean+".") {
				rels = append(rels, historyRelDir+"/"+e.Name())
			}
		}
	}
	var errs []error
	for _, rel := range rels {
		if err := root.Remove(filepath.FromSlash(rel)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// LaunchElephant starts an Elephant. Without a slug it starts a new spec
// named after the goal; with one it works on that spec.
func (a *AgentService) LaunchElephant(req ElephantRequest) (terminal.SessionInfo, error) {
	p, err := project(req.ProjectID)
	if err != nil {
		return terminal.SessionInfo{}, err
	}
	goal := strings.TrimSpace(req.Prompt)
	var slug string
	if strings.TrimSpace(req.Slug) == "" {
		if goal == "" {
			return terminal.SessionInfo{}, errors.New("describe the feature to specify")
		}
		base, err := slugFor(firstLine(goal, 80))
		if err != nil {
			return terminal.SessionInfo{}, err
		}
		slug = freeSlug(p.Path, base)
	} else if slug, err = normalizeSpecSlug(req.Slug); err != nil {
		return terminal.SessionInfo{}, err
	}
	if s := runningEGM(p.ID, slug, "elephant"); len(s) > 0 {
		return terminal.SessionInfo{}, errors.New("an Elephant is already working on this spec: send it a message instead")
	}
	rel := specRelPath(slug)
	existing, err := readSpecIn(p.Path, slug, false)
	revising := err == nil
	if !revising {
		if !errors.Is(err, os.ErrNotExist) {
			return terminal.SessionInfo{}, err
		}
		if goal == "" {
			return terminal.SessionInfo{}, fmt.Errorf("spec %q not found", slug)
		}
		// A skeleton first, so the panel has something to show at once.
		if err := writeSpecFile(p.Path, rel, []byte(specSkeleton(firstLine(goal, 72), goal))); err != nil {
			return terminal.SessionInfo{}, err
		}
	}
	excludeSpecs(p.Path)
	title := "Spec · " + firstLine(cmp.Or(goal, slug), 38)
	if revising {
		title = "Spec · " + firstLine(existing.Title, 38)
	}
	meta := withMeta(req.Meta, map[string]string{"role": "elephant", "spec": slug, "specPath": rel})
	return a.launchSession(LaunchRequest{
		ProjectID: p.ID,
		Agent:     req.Agent,
		Prompt:    elephantPrompt(rel, goal, revising),
		Title:     title,
		Meta:      meta,
	}, nil)
}

// ReviseSpec hands the latest review back to the Elephant: to the one
// working on the spec when there is one, else to a new Elephant. It
// answers the Elephant's session.
func (a *AgentService) ReviseSpec(projectID, slug, agent string) (terminal.SessionInfo, error) {
	p, err := project(projectID)
	if err != nil {
		return terminal.SessionInfo{}, err
	}
	clean, err := normalizeSpecSlug(slug)
	if err != nil {
		return terminal.SessionInfo{}, err
	}
	sf, err := readSpecIn(p.Path, clean, false)
	if err != nil {
		return terminal.SessionInfo{}, fmt.Errorf("spec %q not found", clean)
	}
	if !sf.HasReview {
		return terminal.SessionInfo{}, errors.New("this spec has no review yet: run a Goldfish review first")
	}
	if s := runningEGM(p.ID, clean, "goldfish"); len(s) > 0 {
		return terminal.SessionInfo{}, errors.New("the Goldfish is still reviewing this spec")
	}
	msg := reviseMessage(specRelPath(clean), reviewRelPath(clean))
	if s := runningEGM(p.ID, clean, "elephant"); len(s) > 0 {
		return s[0], a.Send(s[0].ID, msg)
	}
	return a.LaunchElephant(ElephantRequest{ProjectID: p.ID, Agent: agent, Slug: clean, Prompt: msg})
}

// LaunchGoldfish starts a cold review of a spec. The previous review moves
// to the history folder, so the panel never shows it as the new one.
func (a *AgentService) LaunchGoldfish(req GoldfishRequest) (terminal.SessionInfo, error) {
	p, err := project(req.ProjectID)
	if err != nil {
		return terminal.SessionInfo{}, err
	}
	slug, err := normalizeSpecSlug(req.Slug)
	if err != nil {
		return terminal.SessionInfo{}, err
	}
	sf, err := readSpecIn(p.Path, slug, false)
	if err != nil {
		return terminal.SessionInfo{}, fmt.Errorf("spec %q not found", slug)
	}
	if s := runningEGM(p.ID, slug, "goldfish"); len(s) > 0 {
		return terminal.SessionInfo{}, errors.New("a Goldfish is already reviewing this spec")
	}
	if err := archive(p.Path, reviewRelPath(slug), slug+".review"); err != nil {
		return terminal.SessionInfo{}, err
	}
	sRel, rRel := specRelPath(slug), reviewRelPath(slug)
	meta := withMeta(req.Meta, map[string]string{"role": "goldfish", "stateless": "1", "spec": slug, "specPath": sRel, "reviewPath": rRel})
	return a.launchSession(LaunchRequest{
		ProjectID: p.ID,
		Agent:     req.Agent,
		Prompt:    goldfishPrompt(sRel, rRel, strings.TrimSpace(req.Focus), sf.Rounds+1),
		Title:     fmt.Sprintf("Review %d · %s", sf.Rounds+1, firstLine(sf.Title, 34)),
		Meta:      meta,
	}, nil)
}

// LaunchFromSpec hands an approved spec to one builder per agent, each in
// its own worktree with a copy of the spec and its review. A spec without
// a READY review of its current version needs Force.
func (a *AgentService) LaunchFromSpec(req SpecLaunchRequest, agents []string) ([]terminal.SessionInfo, error) {
	p, err := project(req.ProjectID)
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return nil, errors.New("pick at least one agent")
	}
	slug, err := normalizeSpecSlug(req.Slug)
	if err != nil {
		return nil, err
	}
	sf, err := readSpecIn(p.Path, slug, true)
	if err != nil {
		return nil, fmt.Errorf("spec %q not found", slug)
	}
	if !sf.Approved && !req.Force {
		return nil, errors.New(notApproved(sf))
	}
	for _, s := range runningEGM(p.ID, slug, "elephant") {
		// An Elephant waiting for the user is fine; one still writing would
		// change the spec under the builders.
		if Status(s, time.Now()) == "working" && !req.Force {
			return nil, errors.New("the Elephant is still writing this spec: wait until it is done")
		}
	}
	excludeSpecs(p.Path)
	sRel, rRel := specRelPath(slug), reviewRelPath(slug)
	copySpec := func(dir string) error {
		if err := writeSpecFile(dir, sRel, []byte(sf.Content)); err != nil {
			return fmt.Errorf("copy the spec into the worktree: %w", err)
		}
		if sf.HasReview {
			if err := writeSpecFile(dir, rRel, []byte(sf.Review)); err != nil {
				return fmt.Errorf("copy the review into the worktree: %w", err)
			}
		}
		return nil
	}
	prompt := builderPrompt(sRel, rRel, sf.HasReview, strings.TrimSpace(req.Notes))
	worktree := req.Worktree || len(agents) > 1
	var out []terminal.SessionInfo
	var errs []error
	for _, id := range agents {
		s, err := a.launchSession(LaunchRequest{
			ProjectID:   p.ID,
			Agent:       id,
			Prompt:      prompt,
			Title:       firstLine(sf.Title, 44),
			Worktree:    worktree,
			InstallDeps: req.InstallDeps,
			Base:        req.Base,
			Meta:        withMeta(req.Meta, map[string]string{"role": "builder", "spec": slug, "specPath": sRel}),
		}, copySpec)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		out = append(out, s)
	}
	return out, errors.Join(errs...)
}

func notApproved(sf SpecFile) string {
	switch {
	case !sf.HasReview:
		return "this spec has not been reviewed: run a Goldfish review first (or build it anyway)"
	case sf.Stale:
		return "the spec changed after its review: review it again (or build it anyway)"
	case sf.Verdict == "":
		return "the review names no verdict: review the spec again (or build it anyway)"
	}
	return "the review says " + sf.Verdict + ": revise the spec and review it again (or build it anyway)"
}

// LaunchDriftCheck starts a Goldfish auditor in a builder's folder that
// compares its changes with the spec and writes <slug>.drift.md there.
func (a *AgentService) LaunchDriftCheck(req DriftRequest) (terminal.SessionInfo, error) {
	p, err := project(req.ProjectID)
	if err != nil {
		return terminal.SessionInfo{}, err
	}
	slug, err := normalizeSpecSlug(req.Slug)
	if err != nil {
		return terminal.SessionInfo{}, err
	}
	sf, err := readSpecIn(p.Path, slug, true)
	if err != nil {
		return terminal.SessionInfo{}, fmt.Errorf("spec %q not found", slug)
	}
	dir := cmp.Or(strings.TrimSpace(req.Worktree), p.Path)
	if !projectFolder(p, dir) {
		return terminal.SessionInfo{}, errors.New("that folder is not this project or one of its worktrees")
	}
	for _, s := range runningEGM(p.ID, slug, "drift") {
		if samePath(s.Cwd, dir) {
			return terminal.SessionInfo{}, errors.New("a drift check is already running in this worktree")
		}
	}
	sRel, dRel := specRelPath(slug), driftRelPath(slug)
	if !samePath(dir, p.Path) {
		// The auditor checks against the spec as it is now, which may have
		// changed since the builder got its copy.
		if err := writeSpecFile(dir, sRel, []byte(sf.Content)); err != nil {
			return terminal.SessionInfo{}, fmt.Errorf("copy the spec into the worktree: %w", err)
		}
	}
	if err := archive(dir, dRel, slug+".drift"); err != nil {
		return terminal.SessionInfo{}, err
	}
	meta := withMeta(req.Meta, map[string]string{"role": "drift", "stateless": "1", "spec": slug, "specPath": sRel, "driftPath": dRel})
	if !samePath(dir, p.Path) {
		meta["worktree"] = dir
		if b, err := proc.Git(dir, "branch", "--show-current"); err == nil {
			meta["branch"] = strings.TrimSpace(b)
		}
	}
	return a.launchSession(LaunchRequest{
		ProjectID: p.ID,
		Agent:     req.Agent,
		Prompt:    driftPrompt(sRel, dRel, driftBase(p, dir)),
		Title:     "Drift · " + firstLine(sf.Title, 36),
		Cwd:       dir,
		Meta:      meta,
	}, nil)
}

// driftBase is what the auditor diffs against: the main checkout's branch
// for a worktree, nothing (the uncommitted changes) for the project itself.
func driftBase(p store.Project, dir string) string {
	if samePath(dir, p.Path) {
		return ""
	}
	b, err := proc.Git(p.Path, "branch", "--show-current")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(b)
}

// FixDrift sends a builder its drift report to work through.
func (a *AgentService) FixDrift(sessionID string) error {
	s, ok := findSession(sessionID)
	if !ok {
		return errors.New("agent not found")
	}
	slug := s.Meta["spec"]
	if slug == "" {
		return errors.New("this agent is not building a spec")
	}
	rep, err := a.ReadDrift(s.ProjectID, s.Cwd, slug)
	if err != nil {
		return err
	}
	if rep.Content == "" {
		return errors.New("there is no drift report for this attempt yet: run a drift check first")
	}
	if rep.Verdict == VerdictAligned {
		return errors.New("the drift report says ALIGNED: there is nothing to fix")
	}
	if s := runningIn(s.ProjectID, slug, "drift", s.Cwd); s != nil {
		return errors.New("the drift check is still running")
	}
	return a.Send(s.ID, fixDriftMessage(specRelPath(slug), driftRelPath(slug), rep.Verdict))
}

// ReadDrift reads the drift report in a builder's folder.
func (a *AgentService) ReadDrift(projectID, worktree, slug string) (DriftReport, error) {
	p, err := project(projectID)
	if err != nil {
		return DriftReport{}, err
	}
	clean, err := normalizeSpecSlug(slug)
	if err != nil {
		return DriftReport{}, err
	}
	dir := cmp.Or(strings.TrimSpace(worktree), p.Path)
	if !projectFolder(p, dir) {
		return DriftReport{}, errors.New("that folder is not this project or one of its worktrees")
	}
	rep := DriftReport{Slug: clean, Worktree: dir}
	b, at, err := readIn(dir, driftRelPath(clean))
	if errors.Is(err, os.ErrNotExist) {
		return rep, nil
	} else if err != nil {
		return rep, err
	}
	rep.Content, rep.UpdatedAt = string(b), at
	rep.Verdict = parseVerdict(rep.Content, driftVerdicts)
	if sf, err := readSpecIn(p.Path, clean, false); err == nil && sf.UpdatedAt > at {
		rep.Stale = true
	}
	return rep, nil
}

func readSpecIn(root, slug string, body bool) (SpecFile, error) {
	b, at, err := readIn(root, specRelPath(slug))
	if err != nil {
		return SpecFile{}, err
	}
	content := string(b)
	sf := SpecFile{
		Slug:      slug,
		Title:     specTitle(content, slug),
		Path:      specRelPath(slug),
		AbsPath:   filepath.Join(root, filepath.FromSlash(specRelPath(slug))),
		Summary:   specSummary(content),
		UpdatedAt: at,
		Size:      int64(len(b)),
		Rounds:    archived(root, slug+".review"),
	}
	if body {
		sf.Content = content
	}
	if rb, rat, err := readIn(root, reviewRelPath(slug)); err == nil && strings.TrimSpace(string(rb)) != "" {
		rev := string(rb)
		sf.HasReview, sf.ReviewAt, sf.Rounds = true, rat, sf.Rounds+1
		sf.Verdict = parseVerdict(rev, reviewVerdicts)
		sf.Stale = sf.UpdatedAt > sf.ReviewAt
		sf.Approved = sf.Verdict == VerdictReady && !sf.Stale
		if body {
			sf.Review = rev
		}
	}
	return sf, nil
}

// readIn reads root/rel without following symlinks out of root, with its
// modification time in milliseconds.
func readIn(root, rel string) ([]byte, int64, error) {
	b, err := safefile.ReadRegularIn(root, filepath.FromSlash(rel), maxSpecBytes)
	if err != nil {
		return nil, 0, err
	}
	var at int64
	if st, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
		at = st.ModTime().UnixMilli()
	}
	return b, at, nil
}

// writeSpecFile writes root/rel through os.Root (a symlink can't send it out
// of root), by way of a temporary file so a reader never sees half of it.
func writeSpecFile(rootDir, rel string, data []byte) error {
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return err
	}
	defer root.Close()
	rel = filepath.FromSlash(rel)
	if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", rel, time.Now().UnixNano())
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = root.Rename(tmp, rel)
	}
	if err != nil {
		_ = root.Remove(tmp)
	}
	return err
}

// archive moves root/rel, when it exists, to the history folder as
// <name>.<n>.md.
func archive(rootDir, rel, name string) error {
	if _, err := os.Lstat(filepath.Join(rootDir, filepath.FromSlash(rel))); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(filepath.FromSlash(historyRelDir), 0o755); err != nil {
		return err
	}
	dst := fmt.Sprintf("%s/%s.%d.md", historyRelDir, name, archived(rootDir, name)+1)
	return root.Rename(filepath.FromSlash(rel), filepath.FromSlash(dst))
}

// archived counts the history folder's <name>.<n>.md files.
func archived(root, name string) int {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(historyRelDir)))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if rest, ok := strings.CutPrefix(e.Name(), name+"."); ok && strings.HasSuffix(rest, ".md") && !strings.Contains(strings.TrimSuffix(rest, ".md"), ".") {
			n++
		}
	}
	return n
}

// excludeSpecs adds the spec folder to the repository's .git/info/exclude,
// which every worktree shares, so specs never show up as changes to commit.
// .larvyn/project.json, the team's project file, stays tracked.
func excludeSpecs(root string) {
	common, err := proc.Git(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return
	}
	const line = "/" + specsRelDir + "/"
	file := filepath.Join(strings.TrimSpace(common), "info", "exclude")
	b, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == line {
			return
		}
	}
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		b = append(b, '\n')
	}
	b = append(b, "# Larvyn specs (local working files)\n"+line+"\n"...)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err == nil {
		_ = os.WriteFile(file, b, 0o644)
	}
}

// verdictLine finds "Verdict: X" at the start of a line, markdown around
// it allowed: "**Verdict:** READY", "## Verdict: NEEDS REVISION".
var verdictLine = regexp.MustCompile("(?im)^[\\s>#*_`-]*verdict[\\s*_`]*[:：][\\s*_`]*([a-z][a-z _-]*)")

// parseVerdict reads the first "Verdict:" line of a report and maps it to
// one of allowed. Only that line counts: words like "already" or "blocked"
// elsewhere in the report must not change the verdict. A line that lists
// the choices ("READY | BLOCKED") is the template, not a verdict.
func parseVerdict(md string, allowed []string) string {
	for _, m := range verdictLine.FindAllStringSubmatchIndex(md, -1) {
		lineEnd := strings.IndexByte(md[m[0]:], '\n')
		if lineEnd < 0 {
			lineEnd = len(md) - m[0]
		}
		if strings.Contains(md[m[0]:m[0]+lineEnd], "|") {
			continue
		}
		word := strings.Join(strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(strings.ToUpper(md[m[2]:m[3]]))), " ")
		for _, v := range allowed {
			if word == v || strings.HasPrefix(word, v+" ") {
				return v
			}
		}
		return ""
	}
	return ""
}

func specTitle(md, fallback string) string {
	for _, line := range strings.Split(md, "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok && strings.TrimSpace(h) != "" {
			return firstLine(strings.TrimSpace(h), 72)
		}
	}
	return strings.ReplaceAll(fallback, "-", " ")
}

func specSummary(md string) string {
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "_") {
			continue
		}
		return firstLine(t, 120)
	}
	return ""
}

// projectFolder reports whether dir is the project or one of its worktrees.
func projectFolder(p store.Project, dir string) bool {
	if samePath(dir, p.Path) {
		return true
	}
	for _, s := range terminal.Sessions.List() {
		if s.ProjectID == p.ID && s.Meta["worktree"] != "" && samePath(s.Meta["worktree"], dir) {
			return true
		}
	}
	out, err := proc.Git(p.Path, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if wt, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok && samePath(wt, dir) {
			return true
		}
	}
	return false
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		return filepath.Clean(p)
	}
	return strings.EqualFold(resolve(a), resolve(b))
}

func findSession(id string) (terminal.SessionInfo, bool) {
	for _, s := range terminal.Sessions.List() {
		if s.ID == id {
			return s, true
		}
	}
	return terminal.SessionInfo{}, false
}

// runningEGM lists the sessions of a spec still at work, of one role or of
// all. A Goldfish or an auditor that wrote its report and went quiet is done,
// though agents like Antigravity keep their TUI open afterwards.
func runningEGM(projectID, slug, role string) []terminal.SessionInfo {
	var out []terminal.SessionInfo
	for _, s := range terminal.Sessions.List() {
		if s.Running && s.ProjectID == projectID && s.Meta["spec"] == slug && s.Meta["role"] != "" && (role == "" || s.Meta["role"] == role) && !reported(s) {
			out = append(out, s)
		}
	}
	return out
}

// reported reports whether a stateless session (Goldfish, auditor) wrote its
// report since it started and has stopped working.
func reported(s terminal.SessionInfo) bool {
	var dir, rel string
	switch s.Meta["role"] {
	case "goldfish":
		p, ok := store.FindProject(s.ProjectID)
		if !ok {
			return false
		}
		dir, rel = p.Path, reviewRelPath(s.Meta["spec"])
	case "drift":
		dir, rel = s.Cwd, driftRelPath(s.Meta["spec"])
	default:
		return false
	}
	st, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil && st.ModTime().UnixMilli() >= s.StartedAt && Status(s, time.Now()) != "working"
}

// slugFor makes a new spec's name from its title: at most 40 characters, cut
// between words.
func slugFor(title string) (string, error) {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 40 {
		s = s[:41]
		if i := strings.LastIndexByte(s, '-'); i > 10 {
			s = s[:i]
		}
		s = strings.Trim(s[:min(len(s), 40)], "-")
	}
	return normalizeSpecSlug(s)
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func runningIn(projectID, slug, role, dir string) *terminal.SessionInfo {
	for _, s := range runningEGM(projectID, slug, role) {
		if samePath(s.Cwd, dir) {
			return &s
		}
	}
	return nil
}

func withMeta(extra, own map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range extra {
		out[k] = v
	}
	for k, v := range own {
		out[k] = v
	}
	return out
}

// specsWatch emits "specs:changed" with a project's id when a file of its
// specs (or a drift report in one of its worktrees) changes, so the panel
// follows the agents writing them without polling.
var specsWatch sync.Once

func init() {
	application.RegisterEvent[string]("specs:changed")
}

// ServiceStartup starts the specs watcher.
func (a *AgentService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	specsWatch.Do(func() { go watchSpecs(ctx) })
	return nil
}

func watchSpecs(ctx context.Context) {
	seen := map[string]string{}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		dirs := map[string][]string{}
		for _, p := range store.Projects() {
			dirs[p.ID] = []string{p.Path}
		}
		for _, s := range terminal.Sessions.List() {
			if s.Meta["spec"] != "" && s.Meta["worktree"] != "" {
				dirs[s.ProjectID] = append(dirs[s.ProjectID], s.Meta["worktree"])
			}
		}
		for id, list := range dirs {
			var sig strings.Builder
			for _, d := range list {
				sig.WriteString(specsSignature(d))
			}
			if prev, ok := seen[id]; ok && prev != sig.String() {
				if app := application.Get(); app != nil {
					app.Event.Emit("specs:changed", id)
				}
			}
			seen[id] = sig.String()
		}
	}
}

func specsSignature(dir string) string {
	entries, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(specsRelDir)))
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() && strings.HasSuffix(e.Name(), ".md") {
			fmt.Fprintf(&b, "%s|%d|%d;", e.Name(), info.Size(), info.ModTime().UnixNano())
		}
	}
	return b.String()
}

// specSkeleton is a new spec's starting point: the sections the Elephant is
// asked to fill in.
func specSkeleton(title, goal string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n> Status: drafting\n\n## Goal & Context\n\n%s\n", title, cmp.Or(goal, "_Describe the problem and the user journey._"))
	for _, s := range specSections[1:] {
		fmt.Fprintf(&b, "\n## %s\n\n_To do._\n", s.name)
	}
	return b.String()
}

var specSections = []struct{ name, what string }{
	{"Goal & Context", "the problem, who it is for, and what is out of scope"},
	{"Existing Architecture", "the exact files, models and patterns this builds on"},
	{"Data Model & Migrations", "tables, columns and types, indexes, foreign keys, how existing rows migrate"},
	{"Eloquent Models & Relationships", "relationships, casts, scopes, fillable/guarded"},
	{"Routes, Controllers / Actions & Validation", "endpoints, middleware, Form Requests, responses"},
	{"UI", "pages and components touched, in the frontend stack the app already uses"},
	{"Authorization & Security", "policies, gates, tenant isolation, mass assignment"},
	{"Edge Cases & Invariants", "N+1 queries, transactions, concurrency, failures"},
	{"Verification & Tests", "the Pest/PHPUnit feature and unit tests, by name and what each asserts"},
}

func elephantPrompt(specRel, goal string, revising bool) string {
	var b strings.Builder
	b.WriteString("You are THE ELEPHANT, the spec architect of an Elephant-Goldfish workflow in this Laravel app. You keep the whole context of this conversation; others will build from what you write.\n\n")
	if revising {
		b.WriteString("Work on the existing spec in `" + specRel + "`.\n\n")
	}
	if goal != "" {
		b.WriteString("Task:\n" + goal + "\n\n")
	}
	b.WriteString("1. Explore the codebase first (models, migrations, controllers/actions, Form Requests, policies, routes, the frontend stack, tests) so the spec follows the conventions already in use, citing real files.\n")
	b.WriteString("2. Write a complete, concrete spec in `" + specRel + "`, with a `# Title` and these sections:\n")
	for _, s := range specSections {
		b.WriteString("   - `## " + s.name + "`: " + s.what + "\n")
	}
	b.WriteString("3. Keep it proportional: a small feature gets a short spec. Describe behavior, rules, signatures and test names; don't write out whole implementations.\n")
	b.WriteString("4. Ask me in this terminal when something is ambiguous, and keep the spec up to date as we go.\n")
	b.WriteString("5. When it is complete, change its `> Status:` line to `> Status: ready for review` and say so.\n\n")
	b.WriteString("Rule: write NO application code. Create or change no file other than `" + specRel + "`.\n")
	return b.String()
}

func reviseMessage(specRel, reviewRel string) string {
	return "A Goldfish reviewer read the spec cold and wrote its review to `" + reviewRel + "`. Read it and update `" + specRel +
		"`: fix every Blocking Issue (for one you reject, add a short note in the spec saying why) and take a Suggestion only when it is cheap and clearly better. Keep the spec as short as the feature allows. " +
		"Then set its `> Status:` line to `> Status: ready for review`. Write no application code and change no file other than the spec."
}

func goldfishPrompt(specRel, reviewRel, focus string, round int) string {
	var b strings.Builder
	b.WriteString("You are THE GOLDFISH, a stateless reviewer in an Elephant-Goldfish workflow. You have no memory of how the spec was written, and that is the point: read `" + specRel + "` cold and check every claim in it against the actual repository.\n\n")
	if focus != "" {
		b.WriteString("Look especially at:\n" + focus + "\n\n")
	}
	b.WriteString("Check: what a builder would have to guess; the data model (foreign keys, indexes, casts, N+1 queries, migrations); authorization and validation (policies, IDOR, mass assignment); the codebase's conventions; the tests, failure paths included.\n\n")
	b.WriteString("Sort what you find by whether it blocks:\n")
	b.WriteString("- Blocking: a builder following the spec would build the wrong thing, or would have to guess; a security or authorization gap; data loss; a contradiction with the code; a decision only the user can make.\n")
	b.WriteString("- Suggestion: everything else (style, naming, extra tests, nicer code, more detail). Suggestions never block.\n")
	b.WriteString("A spec does not have to be perfect, only correct and unambiguous. Don't ask for more detail than the feature needs.\n\n")
	if round >= 3 {
		fmt.Fprintf(&b, "This is review round %d: earlier rounds already went through this spec. Only flag as blocking what truly blocks.\n\n", round)
	}
	b.WriteString("Write your review to `" + reviewRel + "`. Its first line is exactly one of:\n")
	b.WriteString("  Verdict: READY            (no blocking issues; suggestions are fine)\n")
	b.WriteString("  Verdict: NEEDS REVISION   (blocking issues the spec can fix)\n")
	b.WriteString("  Verdict: BLOCKED          (it needs a decision from the user, or can't work as asked)\n")
	b.WriteString("Then these sections:\n")
	b.WriteString("- `## Blocking Issues`: numbered, each with the exact edit to make in the spec (\"None\" when there are none)\n")
	b.WriteString("- `## Suggestions`: optional improvements, short\n\n")
	b.WriteString("Rules: change neither `" + specRel + "` nor any application code, and don't read other files in `" + specsRelDir + "/` (older reviews would bias you). Write only `" + reviewRel + "`, then sum up your verdict in this terminal.\n")
	return b.String()
}

func builderPrompt(specRel, reviewRel string, hasReview bool, notes string) string {
	var b strings.Builder
	b.WriteString("Implement the feature specified in `" + specRel + "`.\n\n")
	if hasReview {
		b.WriteString("Its review is in `" + reviewRel + "`: the spec wins where they differ; take its Suggestions where they are cheap.\n\n")
	}
	if notes != "" {
		b.WriteString("Additional instructions:\n" + notes + "\n\n")
	}
	b.WriteString("1. Implement every data model, migration, relationship, route, validation rule, policy and edge case in the spec, following this codebase's conventions.\n")
	b.WriteString("2. Write the tests the spec lists and run them (`larvyn test` when it is available, else `php artisan test`) until they pass.\n")
	b.WriteString("3. Make no changes the spec doesn't call for. If the spec is wrong or can't be done as written, stop and tell me instead of working around it.\n")
	b.WriteString("4. Don't edit or commit anything in `" + specsRelDir + "/`.\n")
	return b.String()
}

func driftPrompt(specRel, driftRel, base string) string {
	var b strings.Builder
	b.WriteString("You are THE GOLDFISH AUDITOR of an Elephant-Goldfish workflow, with no memory of how this code was written. Compare the spec in `" + specRel + "` with the changes in this folder: ")
	if base != "" {
		b.WriteString("`git diff " + base + "...HEAD` for what is committed, plus `git status` and `git diff HEAD` for what is not.\n\n")
	} else {
		b.WriteString("`git status` and `git diff HEAD`.\n\n")
	}
	b.WriteString("Write the report to `" + driftRel + "`. Its first line is exactly one of:\n")
	b.WriteString("  Verdict: ALIGNED       (everything in the spec is done, nothing else was changed)\n")
	b.WriteString("  Verdict: MINOR DRIFT   (small gaps or extras)\n")
	b.WriteString("  Verdict: MAJOR DRIFT   (requirements missing or done differently)\n")
	b.WriteString("Then these sections:\n")
	b.WriteString("- `## Requirement Checklist`: every requirement of the spec as `[x]` done, `[~]` done differently or `[ ]` missing, citing file:line\n")
	b.WriteString("- `## Unspecified Changes`: changes the spec doesn't justify\n")
	b.WriteString("- `## Tests`: whether the tests the spec lists exist, and whether they pass (you may run them)\n")
	b.WriteString("- `## Fixes`: a numbered list of what the builder must do to get to ALIGNED\n\n")
	b.WriteString("Rule: change no application code. Write only `" + driftRel + "`, then sum up your verdict in this terminal.\n")
	return b.String()
}

func fixDriftMessage(specRel, driftRel, verdict string) string {
	v := ""
	if verdict != "" {
		v = " (" + verdict + ")"
	}
	return "A stateless auditor compared your changes with `" + specRel + "` and wrote its report to `" + driftRel + "`" + v +
		". Read it and work through its Fixes: implement each `[ ]` missing requirement, bring each `[~]` in line with the spec, and undo changes the spec doesn't call for unless they are needed. " +
		"Run the tests again until they pass. Don't edit anything in `" + specsRelDir + "/`."
}
