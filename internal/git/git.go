// Package git is the Source control service: status, diffs, commits,
// branches and the worktrees agents work in.
package git

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"larvyn/internal/dotenv"
	"larvyn/internal/proc"
	"larvyn/internal/store"
)

type GitService struct{}

// CloseSessionsIn stops the terminals running in a folder before its worktree
// is removed. main wires it to the terminal service, so this package does not
// depend on it.
var CloseSessionsIn = func(dir string) {}

// GitFile is one changed path.
type GitFile struct {
	Path     string `json:"path"`
	Index    string `json:"index"`
	Worktree string `json:"worktree"`
	Added    int    `json:"added"`
	Removed  int    `json:"removed"`
}

// GitStatus summarises a working tree.
type GitStatus struct {
	IsRepo   bool       `json:"isRepo"`
	Branch   string     `json:"branch"`
	Upstream string     `json:"upstream"`
	Ahead    int        `json:"ahead"`
	Behind   int        `json:"behind"`
	Files    []GitFile  `json:"files"`
	Commits  []Commit   `json:"commits"`
	Branches []string   `json:"branches"`
	Stashes  int        `json:"stashes"`
	Worktree []Worktree `json:"worktrees"`
}

type Commit struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    int64  `json:"date"`
	Subject string `json:"subject"`
}

// Worktree is a git worktree; Larvyn gives each agent task one.
type Worktree struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Head     string `json:"head"`
	Main     bool   `json:"main"`
	Managed  bool   `json:"managed"`
	Changes  int    `json:"changes"`
	Ahead    int    `json:"ahead"`
	Detached bool   `json:"detached"`
}

func gitDir(path string) string {
	if p, ok := store.FindProject(path); ok {
		return p.Path
	}
	return path
}

// Status returns branch, changes, recent commits and worktrees. target is a
// project ID or a worktree path.
func (g *GitService) Status(target string) (GitStatus, error) {
	dir := gitDir(target)
	st := GitStatus{}
	out, err := proc.Git(dir, "status", "--porcelain=v1", "-z", "-b", "-uall")
	if err != nil {
		return st, nil
	}
	st.IsRepo = true
	st.Files = parseStatusZ(out, &st)
	if num, err := proc.Git(dir, "diff", "HEAD", "--numstat", "-z"); err == nil {
		stats := parseNumstatZ(num)
		for i := range st.Files {
			s := stats[st.Files[i].Path]
			st.Files[i].Added, st.Files[i].Removed = s[0], s[1]
		}
	}
	if log, err := proc.Git(dir, "log", "-n", "30", "--pretty=format:%h\x1f%an\x1f%at\x1f%s"); err == nil {
		for _, l := range proc.NonEmptyLines(log) {
			f := strings.Split(l, "\x1f")
			if len(f) == 4 {
				t, _ := strconv.ParseInt(f[2], 10, 64)
				st.Commits = append(st.Commits, Commit{Hash: f[0], Author: f[1], Date: t * 1000, Subject: f[3]})
			}
		}
	}
	if br, err := proc.Git(dir, "branch", "--format=%(refname:short)"); err == nil {
		st.Branches = proc.NonEmptyLines(br)
	}
	if sl, err := proc.Git(dir, "stash", "list"); err == nil {
		st.Stashes = len(proc.NonEmptyLines(sl))
	}
	st.Worktree, _ = g.Worktrees(target)
	return st, nil
}

var aheadRe = regexp.MustCompile(`ahead (\d+)`)
var behindRe = regexp.MustCompile(`behind (\d+)`)

func parseBranchLine(s string, st *GitStatus) {
	head, rest, _ := strings.Cut(s, " ")
	branch, upstream, _ := strings.Cut(head, "...")
	st.Branch = strings.TrimPrefix(branch, "No commits yet on ")
	st.Upstream = upstream
	if m := aheadRe.FindStringSubmatch(rest); m != nil {
		st.Ahead, _ = strconv.Atoi(m[1])
	}
	if m := behindRe.FindStringSubmatch(rest); m != nil {
		st.Behind, _ = strconv.Atoi(m[1])
	}
}

// Diff returns the unified diff of one file (or the whole tree when empty).
func (g *GitService) Diff(target, path string, staged bool) (string, error) {
	dir := gitDir(target)
	args := []string{"diff", "--no-color", "--no-ext-diff"}
	if staged {
		args = append(args, "--cached")
	} else {
		args = append(args, "HEAD")
	}
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := proc.Git(dir, args...)
	if err == nil && out == "" && path != "" {
		// Untracked file: show it as all-new.
		if b, rerr := os.ReadFile(filepath.Join(dir, path)); rerr == nil && len(b) < 1<<20 {
			lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
			var sb strings.Builder
			sb.WriteString("new file " + path + "\n@@ -0,0 +1," + strconv.Itoa(len(lines)) + " @@\n")
			for _, l := range lines {
				sb.WriteString("+" + l + "\n")
			}
			return sb.String(), nil
		}
	}
	return out, err
}

// BranchDiff compares a worktree, including uncommitted work, with the point
// where it left base: the changes an agent made.
func (g *GitService) BranchDiff(worktreePath, base string) (string, error) {
	if base == "" {
		base = "HEAD"
	}
	mb, err := proc.Git(worktreePath, "merge-base", base, "HEAD")
	if err != nil {
		return "", err
	}
	// Intent-to-add makes new files show up in the diff without staging them.
	_, _ = proc.Git(worktreePath, append([]string{"add", "-A", "-N", "--", "."}, notCopied...)...)
	return proc.Git(worktreePath, "diff", "--no-color", strings.TrimSpace(mb))
}

// Commit stages everything (or the given paths) and commits.
func (g *GitService) Commit(target, message string, paths []string) error {
	dir := gitDir(target)
	if strings.TrimSpace(message) == "" {
		return errors.New("commit message is empty")
	}
	add := append([]string{"add", "-A", "--"}, paths...)
	if _, err := proc.Git(dir, add...); err != nil {
		return err
	}
	_, err := proc.Git(dir, "commit", "-m", message)
	return err
}

// Discard throws away changes to a path, as `git status` reports it
// (relative to the repository root). Untracked files are deleted, but only
// the exact file git lists as untracked, inside the repository; anything
// else is restored from HEAD.
func (g *GitService) Discard(target, path string) error {
	dir := gitDir(target)
	rel, err := repoRelative(path)
	if err != nil {
		return err
	}
	top, err := proc.Git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	top = strings.TrimSpace(top)
	out, err := proc.Git(top, "--literal-pathspecs", "status", "--porcelain=v1", "-z", "-uall", "--", rel)
	if err != nil {
		return err
	}
	for _, f := range parseStatusZ(out, nil) {
		if f.Path == rel && f.Index == "?" {
			return os.Remove(filepath.Join(top, filepath.FromSlash(rel)))
		}
	}
	_, err = proc.Git(top, "--literal-pathspecs", "checkout", "HEAD", "--", rel)
	return err
}

// repoRelative checks that path is a plain path inside the repository and
// returns it in git's form (forward slashes, cleaned).
func repoRelative(path string) (string, error) {
	p := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if path == "" || p == "." || p == ".." || strings.HasPrefix(p, "../") || filepath.IsAbs(path) || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%q is not a path inside the repository", path)
	}
	return p, nil
}

// parseStatusZ reads `git status --porcelain=v1 -z` output. With -z paths
// are never quoted and a rename's source is its own entry, so file names
// containing " -> " or quotes come through as they are. st, when not nil,
// receives the branch line (-b).
func parseStatusZ(out string, st *GitStatus) []GitFile {
	var files []GitFile
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if i == 0 && strings.HasPrefix(e, "## ") {
			if st != nil {
				parseBranchLine(strings.TrimPrefix(e, "## "), st)
			}
			continue
		}
		if len(e) < 4 {
			continue
		}
		// Either column can hold the rename: the worktree one does for an
		// intent-to-add file (BranchDiff runs `add -N` in worktrees).
		if e[0] == 'R' || e[0] == 'C' || e[1] == 'R' || e[1] == 'C' {
			i++ // the next entry is where it was renamed or copied from
		}
		files = append(files, GitFile{Path: e[3:], Index: string(e[0]), Worktree: string(e[1])})
	}
	return files
}

// parseNumstatZ reads `git diff --numstat -z` into added/removed per path.
// A rename's line has an empty path followed by the source and destination.
func parseNumstatZ(out string) map[string][2]int {
	stats := map[string][2]int{}
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		f := strings.SplitN(entries[i], "\t", 3)
		if len(f) != 3 {
			continue
		}
		path := f[2]
		if path == "" && i+2 < len(entries) {
			path = entries[i+2]
			i += 2
		}
		a, _ := strconv.Atoi(f[0])
		r, _ := strconv.Atoi(f[1])
		stats[path] = [2]int{a, r}
	}
	return stats
}

// Checkout switches branches, creating it when create is set.
func (g *GitService) Checkout(target, branch string, create bool) error {
	args := []string{"checkout"}
	if create {
		args = append(args, "-b")
	}
	_, err := proc.Git(gitDir(target), append(args, branch)...)
	return err
}

// Worktrees lists the project's worktrees.
func (g *GitService) Worktrees(target string) ([]Worktree, error) {
	dir := gitDir(target)
	out, err := proc.Git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []Worktree
	var cur *Worktree
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			if cur != nil {
				list = append(list, *cur)
			}
			cur = &Worktree{Path: filepath.FromSlash(strings.TrimPrefix(line, "worktree "))}
		case cur == nil:
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")[:7]
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Detached = true
		}
	}
	if cur != nil {
		list = append(list, *cur)
	}
	root := worktreeRoot()
	for i := range list {
		list[i].Main = i == 0
		list[i].Managed = strings.HasPrefix(strings.ToLower(list[i].Path), strings.ToLower(root))
		if st, err := proc.Git(list[i].Path, "status", "--porcelain"); err == nil {
			list[i].Changes = len(proc.NonEmptyLines(st))
		}
		if i > 0 && len(list) > 0 && list[0].Branch != "" && list[i].Branch != "" {
			if n, err := proc.Git(list[i].Path, "rev-list", "--count", list[0].Branch+".."+list[i].Branch); err == nil {
				list[i].Ahead, _ = strconv.Atoi(strings.TrimSpace(n))
			}
		}
	}
	return list, nil
}

func worktreeRoot() string {
	if r := store.CurrentSettings().WorktreeRoot; r != "" {
		return filepath.Clean(r)
	}
	return filepath.Join(store.HomeDir(), "worktrees")
}

// Slug turns a title into a branch or folder name: lowercase, dashes.
func Slug(s string) string { return proc.Slug(s) }

// WorktreeRequest describes a new isolated checkout.
type WorktreeRequest struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Base      string `json:"base"`
	CopyEnv   bool   `json:"copyEnv"`
}

// CreateWorktree adds a worktree on a new larvyn/<name> branch, with the
// project's .env and SQLite database copied so the app runs there too.
func (g *GitService) CreateWorktree(req WorktreeRequest) (Worktree, error) {
	p, ok := store.FindProject(req.ProjectID)
	if !ok {
		return Worktree{}, errors.New("project not found")
	}
	name := Slug(req.Name)
	path := filepath.Join(worktreeRoot(), Slug(p.Name), name)
	for i := 2; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		path = filepath.Join(worktreeRoot(), Slug(p.Name), name+"-"+strconv.Itoa(i))
		name = name + "-" + strconv.Itoa(i)
	}
	branch := "larvyn/" + name
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	args := []string{"worktree", "add", "-b", branch, path}
	if req.Base != "" {
		args = append(args, req.Base)
	}
	if _, err := proc.Git(p.Path, args...); err != nil {
		return Worktree{}, err
	}
	if req.CopyEnv {
		copyFile(filepath.Join(p.Path, ".env"), filepath.Join(path, ".env"))
		env := dotenv.Read(filepath.Join(p.Path, ".env"))
		if env["DB_CONNECTION"] == "sqlite" {
			db := env["DB_DATABASE"]
			if db == "" || !filepath.IsAbs(db) {
				copyFile(filepath.Join(p.Path, "database", "database.sqlite"), filepath.Join(path, "database", "database.sqlite"))
			}
		}
	}
	return Worktree{Path: path, Branch: branch, Managed: true}, nil
}

// RemoveWorktree deletes a worktree and, when asked, its branch.
func (g *GitService) RemoveWorktree(projectID, path string, deleteBranch bool) error {
	p, ok := store.FindProject(projectID)
	if !ok {
		return errors.New("project not found")
	}
	CloseSessionsIn(path)
	branch := ""
	if wts, err := g.Worktrees(projectID); err == nil {
		for _, w := range wts {
			if samePath(w.Path, path) {
				branch = w.Branch
			}
		}
	}
	if _, err := proc.Git(p.Path, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	if deleteBranch && strings.HasPrefix(branch, "larvyn/") {
		_, _ = proc.Git(p.Path, "branch", "-D", branch)
	}
	return nil
}

// samePath reports whether a and b name the same directory. git prints
// worktree paths with symlinks and Windows short names (RUNNER~1) resolved,
// so a plain string compare misses /var vs /private/var on macOS.
func samePath(a, b string) bool {
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		return filepath.Clean(p)
	}
	return strings.EqualFold(resolve(a), resolve(b))
}

// MergeWorktree commits pending work in a worktree and merges its branch
// into the project's current branch.
func (g *GitService) MergeWorktree(projectID, path, message string) (string, error) {
	p, ok := store.FindProject(projectID)
	if !ok {
		return "", errors.New("project not found")
	}
	if st, _ := proc.Git(path, "status", "--porcelain"); len(proc.NonEmptyLines(st)) > 0 {
		if message == "" {
			message = "wip: agent changes"
		}
		if _, err := proc.Git(path, append([]string{"add", "-A", "--", "."}, notCopied...)...); err != nil {
			return "", err
		}
		if _, err := proc.Git(path, "commit", "-m", message); err != nil {
			return "", err
		}
	}
	branch, err := proc.Git(path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return proc.Git(p.Path, "merge", "--no-ff", "--no-edit", strings.TrimSpace(branch))
}

// notCopied excludes the files CreateWorktree copies from the main checkout,
// so they are never committed even where .gitignore misses them.
var notCopied = []string{":!.env", ":!database/database.sqlite"}

func copyFile(from, to string) {
	in, err := os.Open(from)
	if err != nil {
		return
	}
	defer in.Close()
	_ = os.MkdirAll(filepath.Dir(to), 0o755)
	out, err := os.Create(to)
	if err != nil {
		return
	}
	defer out.Close()
	_, _ = io.Copy(out, in)
}
