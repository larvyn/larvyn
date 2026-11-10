package agentapi

import (
	"errors"
	"fmt"
	"strings"

	"larvyn/internal/api"
	"larvyn/internal/proc"
	"larvyn/internal/terminal"
)

func init() {
	api.Register("ports", "List the TCP ports the project's terminals and services listen on, with their URL: which port vite or a dev server an agent started got", apiPorts,
		"all?: every project's terminals")
	api.MarkReadOnly("ports")
}

// PortRow is a port a terminal's processes listen on. Its URL says
// localhost, not 127.0.0.1: a server can listen on ::1 only, as Vite does
// where Node resolves localhost to it, and a browser tries both.
type PortRow struct {
	Terminal string `json:"terminal"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Port     int    `json:"port"`
	URL      string `json:"url"`
}

// PortRows is what ports prints.
type PortRows []PortRow

func (rs PortRows) Text() string {
	if len(rs) == 0 {
		return "No terminal of this project listens on a port.\n"
	}
	var b strings.Builder
	for _, r := range rs {
		fmt.Fprintf(&b, "%-8s %-7s %-24s %s\n", r.Terminal, r.Kind, r.Title, r.URL)
	}
	return b.String()
}

func apiPorts(c api.Caller, all bool) (PortRows, error) {
	var projectID string
	if !all {
		p, _, _, err := callerProject(c)
		if err != nil {
			return nil, fmt.Errorf("%v (or pass --all)", err)
		}
		projectID = p.ID
	}
	var sessions []terminal.SessionInfo
	pids := map[int]terminal.SessionInfo{}
	var roots []int
	for _, s := range terminal.Sessions.List() {
		if !s.Running || (!all && s.ProjectID != projectID) {
			continue
		}
		pid, err := terminal.PID(s.ID)
		if err != nil {
			continue
		}
		sessions = append(sessions, s)
		pids[pid] = s
		roots = append(roots, pid)
	}
	ports, ok := proc.ListeningPorts(roots)
	if !ok {
		return nil, errors.New("the ports in use could not be read here: larvyn context gives the app's URL")
	}
	out := PortRows{}
	for _, s := range sessions {
		for pid, ps := range ports {
			if pids[pid].ID != s.ID {
				continue
			}
			for _, p := range ps {
				out = append(out, PortRow{Terminal: s.ID, Kind: s.Kind, Title: s.Title, Port: p, URL: fmt.Sprintf("http://localhost:%d", p)})
			}
		}
	}
	return out, nil
}
