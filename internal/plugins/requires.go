package plugins

import (
	"fmt"
	"os"
	"runtime"

	"larvyn/internal/proc"
	"larvyn/internal/terminal"
)

// Missing is a required program that is not on PATH, with how to install it
// on this system.
type Missing struct {
	Program string `json:"program"`
	// Install is the command for this system; "" when the plugin gave none.
	Install string `json:"install"`
	URL     string `json:"url"`
}

// osKey is how manifests name the running system.
func osKey() string {
	switch runtime.GOOS {
	case "darwin":
		return "mac"
	case "windows":
		return "windows"
	}
	return "linux"
}

// which finds a program; tests replace it.
var which = proc.Which

// missing returns the programs m requires that are not installed.
func missing(m Manifest) []Missing {
	var out []Missing
	for _, r := range m.Requires {
		if which(r.Program) == "" {
			out = append(out, Missing{Program: r.Program, Install: r.Install[osKey()], URL: r.URL})
		}
	}
	return out
}

// InstallRequirement runs the install command of a plugin's missing program
// in a terminal the user can watch. The frontend shows the exact command and
// asks first; confirmed is that command, and Larvyn refuses to run anything
// else, in case the manifest changed after the user saw it.
func (s *PluginService) InstallRequirement(id, program, confirmed string) (terminal.SessionInfo, error) {
	p, err := find(id)
	if err != nil {
		return terminal.SessionInfo{}, err
	}
	for _, m := range p.Missing {
		if m.Program != program {
			continue
		}
		if m.Install == "" {
			return terminal.SessionInfo{}, fmt.Errorf("%s gives no install command for this system; see %s", p.Manifest.Name, m.URL)
		}
		if m.Install != confirmed {
			return terminal.SessionInfo{}, fmt.Errorf("the install command of %s changed; refresh the plugins and confirm it again", p.Manifest.Name)
		}
		home, _ := os.UserHomeDir()
		return terminal.Sessions.Start(terminal.SessionSpec{Kind: "run", Key: "plugin-require-" + program,
			Title: "Install " + program, Cwd: home, Command: m.Install})
	}
	return terminal.SessionInfo{}, fmt.Errorf("%s is not missing", program)
}
