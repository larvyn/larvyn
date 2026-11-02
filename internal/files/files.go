package files

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"larvyn/internal/proc"
	"larvyn/internal/store"
)

// maxEditableSize is the largest file the editor opens.
const maxEditableSize = 2 << 20

// FileService browses and edits the files of a project, never outside it.
type FileService struct{}

// FileEntry is one item of a folder listing.
type FileEntry struct {
	Name string `json:"name"`
	Path string `json:"path"` // relative to the project, with forward slashes
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
}

// FileContent is a file opened in the editor. Binary and TooLarge files come
// without Content.
type FileContent struct {
	Path    string `json:"path"`
	Abs     string `json:"abs"`
	Content string `json:"content"`
	Size    int64  `json:"size"`
	// Version identifies what is on disk (modification time and size). It is
	// sent back on save to detect outside changes. A string, because unix
	// nanoseconds do not fit a JavaScript number exactly.
	Version  string `json:"version"`
	Binary   bool   `json:"binary"`
	TooLarge bool   `json:"tooLarge"`
}

// hiddenEntries are never listed: they are noise in a code browser.
var hiddenEntries = map[string]bool{".git": true, ".DS_Store": true, "Thumbs.db": true}

// List returns a folder's entries, folders first. rel "" is the project root.
func (s *FileService) List(projectID, rel string) ([]FileEntry, error) {
	root, abs, err := resolveProjectFile(projectID, rel)
	if err != nil {
		return nil, err
	}
	items, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	out := make([]FileEntry, 0, len(items))
	for _, it := range items {
		if hiddenEntries[it.Name()] {
			continue
		}
		full := filepath.Join(abs, it.Name())
		info, err := os.Stat(full) // follows symlinks, e.g. public/storage
		if err != nil {
			continue
		}
		relPath, _ := filepath.Rel(root, full)
		out = append(out, FileEntry{Name: it.Name(), Path: filepath.ToSlash(relPath), Dir: info.IsDir(), Size: info.Size()})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Read opens a file for the editor.
func (s *FileService) Read(projectID, rel string) (FileContent, error) {
	_, abs, err := resolveProjectFile(projectID, rel)
	if err != nil {
		return FileContent{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return FileContent{}, err
	}
	if info.IsDir() {
		return FileContent{}, errors.New(rel + " is a folder")
	}
	fc := FileContent{Path: filepath.ToSlash(filepath.Clean(rel)), Abs: abs, Size: info.Size(), Version: fileVersion(info)}
	if info.Size() > maxEditableSize {
		fc.TooLarge = true
		return fc, nil
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return FileContent{}, err
	}
	if bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
		fc.Binary = true
		return fc, nil
	}
	fc.Content = string(b)
	return fc, nil
}

// Write saves a file. version is what Read returned: when the file changed on
// disk since, nothing is written and the error says so. It returns the new
// version.
func (s *FileService) Write(projectID, rel, content, version string) (string, error) {
	_, abs, err := resolveProjectFile(projectID, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New(rel + " is a folder")
	}
	if version != "" && fileVersion(info) != version {
		return "", errors.New(rel + " changed on disk since it was opened; reload it first")
	}
	if err := os.WriteFile(abs, []byte(content), info.Mode().Perm()); err != nil {
		return "", err
	}
	info, err = os.Stat(abs)
	if err != nil {
		return "", err
	}
	return fileVersion(info), nil
}

// fileVersion changes whenever the file is written: modification time in
// nanoseconds plus size.
func fileVersion(info os.FileInfo) string {
	return strconv.FormatInt(info.ModTime().UnixNano(), 10) + "-" + strconv.FormatInt(info.Size(), 10)
}

// resolveProjectFile turns a project-relative path into an absolute one and
// refuses anything that ends up outside the project, through ".." or a
// symlink.
func resolveProjectFile(projectID, rel string) (root, abs string, err error) {
	root, err = store.ProjectPath(projectID)
	if err != nil {
		return "", "", err
	}
	abs, err = resolveWithin(root, rel)
	return root, abs, err
}

func resolveWithin(root, rel string) (string, error) {
	rel = filepath.FromSlash(strings.TrimSpace(rel))
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", errors.New("paths must be relative to the project")
	}
	abs := filepath.Join(root, rel)
	if !inside(root, abs) {
		return "", errors.New("path is outside the project")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realAbs, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	if !inside(realRoot, realAbs) {
		return "", errors.New("path points outside the project")
	}
	return abs, nil
}

func inside(root, path string) bool {
	r, err := filepath.Rel(root, path)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// FileIndex is every file of a project, for the quick-open finder.
type FileIndex struct {
	Files     []string `json:"files"` // relative to the project, with forward slashes, sorted
	Truncated bool     `json:"truncated"`
}

// maxIndexedFiles caps FileIndex: past it the finder gets a partial list and
// says so. A variable so the tests can lower it.
var maxIndexedFiles = 20000

// skippedDirNames are folders the index never walks into, wherever they are.
var skippedDirNames = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, ".idea": true, ".vscode": true}

// skippedDirPaths are build and cache folders skipped by their place in the
// project.
var skippedDirPaths = map[string]bool{"storage/framework": true, "bootstrap/cache": true, "public/build": true}

// AllFiles lists every file of a project in one call, for the finder. In a git
// repository it asks git, so .gitignore is honoured exactly (tracked and
// untracked files, minus the ignored ones); elsewhere it walks the tree and
// skips .git, dependencies and build output. Nothing outside the project is
// returned: entries that do not exist or resolve elsewhere (symlinks) are
// dropped.
func (s *FileService) AllFiles(projectID string) (FileIndex, error) {
	root, err := store.ProjectPath(projectID)
	if err != nil {
		return FileIndex{}, err
	}
	if _, err := resolveWithin(root, ""); err != nil {
		return FileIndex{}, err
	}
	if idx, ok := gitFileIndex(root); ok {
		return idx, nil
	}
	return walkFileIndex(root)
}

// gitFileIndex asks git for the project's files. ok is false when the folder
// is not in a repository, git is missing, or git lists nothing (for example a
// project its parent repository ignores), so the caller walks instead.
func gitFileIndex(root string) (FileIndex, bool) {
	if proc.Which("git") == "" {
		return FileIndex{}, false
	}
	out, err := proc.Git(root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return FileIndex{}, false
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return FileIndex{}, false
	}
	// --cached and --others can both name a file (e.g. one staged for removal
	// and recreated), so sort and skip repeats.
	sort.Strings(paths)
	ix := newFileIndexer(root)
	prev := ""
	for _, p := range paths {
		if p == prev {
			continue
		}
		prev = p
		ix.addChecked(p)
		if ix.truncated {
			break
		}
	}
	return ix.result(), true
}

// walkFileIndex walks the project without git, skipping the usual noise.
func walkFileIndex(root string) (FileIndex, error) {
	ix := newFileIndexer(root)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		name := d.Name()
		if d.IsDir() {
			// WalkDir does not follow symlinked folders, so every folder it
			// enters really is inside the project.
			if skippedDirNames[name] || skippedDirPaths[rel] {
				return fs.SkipDir
			}
			return nil
		}
		if hiddenEntries[name] {
			return nil
		}
		ix.addChecked(rel)
		if ix.truncated {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return FileIndex{}, err
	}
	return ix.result(), nil
}

// fileIndexer collects index entries up to maxIndexedFiles.
type fileIndexer struct {
	root      string
	files     []string
	truncated bool
	dirOK     map[string]bool // folder -> really inside the project
}

func newFileIndexer(root string) *fileIndexer {
	return &fileIndexer{root: root, files: []string{}, dirOK: map[string]bool{}}
}

// addChecked adds rel (forward slashes) when it is a regular file that really
// is inside the project: its folder resolves inside (checked once per folder)
// and, for a symlink, so does its target. A file past the cap is not added and
// marks the index truncated.
func (ix *fileIndexer) addChecked(rel string) {
	if rel == "" || strings.HasPrefix(rel, "/") {
		return
	}
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	ok, seen := ix.dirOK[dir]
	if !seen {
		_, err := resolveWithin(ix.root, dir)
		ok = err == nil
		ix.dirOK[dir] = ok
	}
	if !ok {
		return
	}
	abs := filepath.Join(ix.root, filepath.FromSlash(rel))
	if !inside(ix.root, abs) {
		return
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return // listed by git but deleted since
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		if _, err := resolveWithin(ix.root, rel); err != nil {
			return // points outside the project, or nowhere
		}
		if info, err = os.Stat(abs); err != nil {
			return
		}
	}
	if !info.Mode().IsRegular() {
		return // folders (e.g. git submodules), sockets, devices
	}
	if len(ix.files) >= maxIndexedFiles {
		ix.truncated = true
		return
	}
	ix.files = append(ix.files, rel)
}

func (ix *fileIndexer) result() FileIndex {
	sort.Strings(ix.files)
	return FileIndex{Files: ix.files, Truncated: ix.truncated}
}
