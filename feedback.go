package main

import (
	"bufio"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Public repository where users report bugs and ask for features; the source
// repository is private.
const feedbackRepo = "https://github.com/larvyn/releases"

// AppInfo describes this build for the About card and bug reports.
type AppInfo struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	LogPath string `json:"logPath"`
}

// Info returns the version, a readable OS description and where larvyn.log is.
func (s *SystemService) Info() AppInfo {
	return AppInfo{Version: version, OS: osDescription(), LogPath: filepath.Join(homeDir(), "larvyn.log")}
}

// ReportBug opens a new bug report in the browser with the version and OS
// filled in. details, when given (a crash message and stack), prefills
// "What happened?".
func (s *SystemService) ReportBug(details string) error {
	return s.OpenURL(bugReportURL(details))
}

// bugReportURL builds the prefilled issue form link. Query keys match the
// field ids in larvyn/releases/.github/ISSUE_TEMPLATE/bug_report.yml.
func bugReportURL(details string) string {
	q := url.Values{}
	q.Set("template", "bug_report.yml")
	q.Set("version", version)
	q.Set("os", osDescription())
	if details = strings.TrimSpace(details); details != "" {
		// Keep the whole URL well under browser and GitHub limits.
		if len(details) > 1500 {
			details = details[:1500] + "\n…"
		}
		q.Set("what-happened", "```\n"+details+"\n```")
	}
	return feedbackRepo + "/issues/new?" + q.Encode()
}

// RequestFeature opens a new feature request in the browser.
func (s *SystemService) RequestFeature() error {
	return s.OpenURL(feedbackRepo + "/issues/new?template=feature_request.yml")
}

// OpenLogFolder shows the folder that holds larvyn.log.
func (s *SystemService) OpenLogFolder() error {
	_ = os.MkdirAll(homeDir(), 0o755)
	return s.OpenFolder(homeDir())
}

// osDescription is "Ubuntu 24.04.5 LTS (linux/amd64)", "macOS 15.1
// (darwin/arm64)" or "Windows 11 (windows/amd64)", falling back to GOOS/GOARCH.
func osDescription() string {
	arch := runtime.GOOS + "/" + runtime.GOARCH
	name := ""
	switch runtime.GOOS {
	case "linux":
		if f, err := os.Open("/etc/os-release"); err == nil {
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
					name = strings.Trim(v, `"`)
				}
			}
			f.Close()
		}
	case "darwin":
		if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			name = "macOS " + strings.TrimSpace(string(out))
		}
	case "windows":
		cmd := exec.Command("cmd", "/c", "ver")
		hideWindow(cmd)
		if out, err := cmd.Output(); err == nil {
			name = strings.TrimSpace(string(out))
		}
	}
	if name == "" {
		return arch
	}
	return name + " (" + arch + ")"
}
