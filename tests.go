package main

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"larvyn/internal/store"
)

// TestCase is one test from a JUnit report written by `artisan test`.
type TestCase struct {
	Name    string  `json:"name"`
	Class   string  `json:"class"`
	File    string  `json:"file"`
	Line    int     `json:"line"`
	Time    float64 `json:"time"`
	Status  string  `json:"status"` // passed, failed, error, skipped
	Message string  `json:"message"`
	Details string  `json:"details"`
}

// TestReport is the result of the last test run.
type TestReport struct {
	Exists bool `json:"exists"`
	// Incomplete is set when the last run left an empty or cut-off report,
	// usually because it stopped before PHPUnit/Pest wrote it.
	Incomplete bool       `json:"incomplete"`
	Modified   int64      `json:"modified"`
	Tests      int        `json:"tests"`
	Passed     int        `json:"passed"`
	Failed     int        `json:"failed"`
	Skipped    int        `json:"skipped"`
	Time       float64    `json:"time"`
	Cases      []TestCase `json:"cases"`
}

type junitSuite struct {
	Suites []junitSuite `xml:"testsuite"`
	Cases  []junitCase  `xml:"testcase"`
}

type junitCase struct {
	Name    string  `xml:"name,attr"`
	Class   string  `xml:"class,attr"`
	File    string  `xml:"file,attr"`
	Line    string  `xml:"line,attr"`
	Time    float64 `xml:"time,attr"`
	Failure *struct {
		Type string `xml:"type,attr"`
		Text string `xml:",chardata"`
	} `xml:"failure"`
	Error *struct {
		Type string `xml:"type,attr"`
		Text string `xml:",chardata"`
	} `xml:"error"`
	Skipped *struct{} `xml:"skipped"`
}

func junitPath(projectID string) string {
	return filepath.Join(store.HomeDir(), "junit", projectID+".xml")
}

// RunTests runs `artisan test` in the console, writing a JUnit report Larvyn
// reads afterwards. filter narrows it to matching tests.
func (l *LaravelService) RunTests(projectID, filter string, parallel bool) (SessionInfo, error) {
	p, ok := store.FindProject(projectID)
	if !ok {
		return SessionInfo{}, errors.New("project not found")
	}
	_ = os.MkdirAll(filepath.Dir(junitPath(projectID)), 0o755)
	argv := []string{store.PHP(), "artisan", "test", "--log-junit=" + junitPath(projectID)}
	title := "artisan test"
	if filter != "" {
		argv = append(argv, "--filter="+filter)
		title += " --filter=" + filter
	}
	if parallel {
		argv = append(argv, "--parallel")
		title += " --parallel"
	}
	return terminals.Start(SessionSpec{ProjectID: p.ID, Kind: "run", Key: "tests", Title: title, Cwd: p.Path, Argv: argv})
}

var failAt = regexp.MustCompile(`(?m)^\s*(?:at\s+)?((?:[A-Za-z]:)?[^\s:]+\.php):(\d+)\s*$`)

// TestResults reads the report of the last run.
func (l *LaravelService) TestResults(projectID string) (TestReport, error) {
	r := TestReport{}
	path := junitPath(projectID)
	fi, err := os.Stat(path)
	if err != nil {
		return r, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	var root struct {
		junitSuite
	}
	if err := xml.Unmarshal(b, &root); err != nil {
		// Empty or truncated: the run was interrupted or crashed on boot.
		// Not an error to toast; the view explains it and offers a re-run.
		r.Incomplete = true
		return r, nil
	}
	r.Exists = true
	r.Modified = fi.ModTime().UnixMilli()
	dir, _ := store.ProjectPath(projectID)
	var walk func(s junitSuite)
	walk = func(s junitSuite) {
		for _, c := range s.Cases {
			tc := TestCase{Name: c.Name, Class: c.Class, File: c.File, Time: c.Time, Status: "passed"}
			tc.Line, _ = strconv.Atoi(c.Line)
			if tc.Class == "" {
				tc.Class = strings.TrimSuffix(filepath.Base(c.File), ".php")
			}
			text := ""
			switch {
			case c.Failure != nil:
				tc.Status, text = "failed", c.Failure.Text
			case c.Error != nil:
				tc.Status, text = "error", c.Error.Text
			case c.Skipped != nil:
				tc.Status = "skipped"
			}
			if text != "" {
				// Pest starts the failure text with the test's own name.
				text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), c.Name))
				tc.Details = text
				tc.Message = strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
				// Point at the first frame inside the project, not vendor/.
				for _, m := range failAt.FindAllStringSubmatch(text, -1) {
					if !strings.Contains(m[1], "vendor") && (dir == "" || strings.HasPrefix(strings.ToLower(m[1]), strings.ToLower(dir)) || !filepath.IsAbs(m[1])) {
						tc.File = m[1]
						tc.Line, _ = strconv.Atoi(m[2])
						break
					}
				}
			}
			r.Cases = append(r.Cases, tc)
			r.Tests++
			r.Time += c.Time
			switch tc.Status {
			case "passed":
				r.Passed++
			case "skipped":
				r.Skipped++
			default:
				r.Failed++
			}
		}
		for _, sub := range s.Suites {
			walk(sub)
		}
	}
	walk(root.junitSuite)
	return r, nil
}
