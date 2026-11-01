package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
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
	Path     string `json:"path"`
	Abs      string `json:"abs"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	ModTime  int64  `json:"modTime"` // unix nanoseconds, sent back on save to detect outside changes
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
	fc := FileContent{Path: filepath.ToSlash(filepath.Clean(rel)), Abs: abs, Size: info.Size(), ModTime: info.ModTime().UnixNano()}
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

// Write saves a file. modTime is what Read returned: when the file changed on
// disk since, nothing is written and the error says so. It returns the new
// modification time.
func (s *FileService) Write(projectID, rel, content string, modTime int64) (int64, error) {
	_, abs, err := resolveProjectFile(projectID, rel)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, errors.New(rel + " is a folder")
	}
	if modTime != 0 && info.ModTime().UnixNano() != modTime {
		return 0, errors.New(rel + " changed on disk since it was opened; reload it first")
	}
	if err := os.WriteFile(abs, []byte(content), info.Mode().Perm()); err != nil {
		return 0, err
	}
	info, err = os.Stat(abs)
	if err != nil {
		return 0, err
	}
	return info.ModTime().UnixNano(), nil
}

// resolveProjectFile turns a project-relative path into an absolute one and
// refuses anything that ends up outside the project, through ".." or a
// symlink.
func resolveProjectFile(projectID, rel string) (root, abs string, err error) {
	root, err = projectPath(projectID)
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
