package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"larvyn/internal/agents"
	"larvyn/internal/api"
	"larvyn/internal/proc"
	"larvyn/internal/store"
	"larvyn/internal/terminal"
)

// Agents driving agents through the API: start one in a new worktree, send
// it a follow-up, read its status and wait until it is done. The child's
// report is its screen, which comes back to the parent through wait; it is
// never typed into the parent's terminal.
func init() {
	api.Register("agents.kinds", "List the agents Larvyn can start and whether each is installed", apiAgentKinds)
	api.Register("agents.list", "List the agents of the caller's project with their status", apiAgentList,
		"all?: every project's agents")
	api.Register("agents.start", "Start an agent on a prompt, in a new worktree of the caller's project", apiAgentStart,
		"prompt: its task",
		"agent?: which agent (agents kinds lists them); the default agent of Settings when left out",
		"base?: the branch the worktree starts from; the caller's branch by default",
		"here?: work in the caller's folder instead of a new worktree",
		"title?: its name in Larvyn")
	api.Register("agents.send", "Type a follow-up message into an agent and press enter", apiAgentSend,
		"id: the agent's terminal (agents list shows them)",
		"text: the message")
	api.Register("agents.status", "Print an agent's status and its screen", apiAgentStatus,
		"id: the agent's terminal",
		"lines?: how many of the screen's last lines, 40 by default")
	api.Register("agents.wait", "Wait until an agent is done or needs attention, then print its screen; exits 0 when it is done, 2 when it needs attention, 1 on a timeout or a crash", apiAgentWait,
		"id: the agent's terminal",
		"timeout?: seconds, 1800 by default",
		"lines?: how many of the screen's last lines, 40 by default")
	api.MarkReadOnly("agents.kinds", "agents.list", "agents.status", "agents.wait")
}

// maxDepth is how many levels of agents the API lets agents start below
// the ones the user started: enough for helpers, not for a runaway chain.
const maxDepth = 2

// prompted is when the API last gave each agent a task, so wait doesn't
// take an agent that hasn't started on it yet for one that is done.
var prompted sync.Map // session id → time.Time

// startGrace is how long an agent may stay quiet after a prompt before
// wait takes it as done.
const startGrace = 20 * time.Second

// quietFor is how long an agent must be idle for wait to take it as done:
// a little longer than WorkingFor, so a short pause doesn't end it.
const quietFor = 3 * time.Second

// AgentKind is a line of `larvyn agents kinds`.
type AgentKind struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Default   bool   `json:"default"`
}

func apiAgentKinds() []AgentKind {
	def := store.CurrentSettings().DefaultAgent
	out := []AgentKind{}
	for _, k := range (&agents.AgentService{}).Kinds() {
		out = append(out, AgentKind{ID: k.ID, Name: k.Name, Installed: k.Installed, Default: k.ID == def})
	}
	return out
}

// AgentRow is an agent in `larvyn agents list`.
type AgentRow struct {
	ID       string `json:"id"`
	Agent    string `json:"agent"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	// Parent is the agent that started this one through the API.
	Parent string `json:"parent,omitempty"`
}

func agentRow(s terminal.SessionInfo) AgentRow {
	return AgentRow{ID: s.ID, Agent: s.Meta["agentName"], Title: s.Title, Status: agents.Status(s, time.Now()),
		Branch: s.Meta["branch"], Worktree: s.Meta["worktree"], Parent: s.Meta["parent"]}
}

func apiAgentList(c api.Caller, all bool) ([]AgentRow, error) {
	var projectID string
	if !all {
		p, _, _, err := callerProject(c)
		if err != nil {
			return nil, fmt.Errorf("%v (or pass --all)", err)
		}
		projectID = p.ID
	}
	out := []AgentRow{}
	for _, s := range terminal.Sessions.List() {
		if s.Kind == "agent" && (all || s.ProjectID == projectID) {
			out = append(out, agentRow(s))
		}
	}
	return out, nil
}

// findAgent is findTerminal for agents only.
func findAgent(id string) (terminal.SessionInfo, error) {
	s, err := findTerminal(id)
	if err != nil {
		return s, err
	}
	if s.Kind != "agent" {
		return s, fmt.Errorf("terminal %s is not an agent (larvyn terminal write types into any terminal)", s.ID)
	}
	return s, nil
}

func apiAgentStart(c api.Caller, prompt, agent, base string, here bool, title string) (AgentRow, error) {
	if strings.TrimSpace(prompt) == "" {
		return AgentRow{}, errors.New("give the agent a prompt")
	}
	p, dir, self, err := callerProject(c)
	if err != nil {
		return AgentRow{}, err
	}
	meta, err := childMeta(self)
	if err != nil {
		return AgentRow{}, err
	}
	agent = defaultAgent(agent)
	req := agents.LaunchRequest{ProjectID: p.ID, Agent: agent, Prompt: prompt, Title: title, Worktree: !here, InstallDeps: true, Meta: meta}
	if here {
		req.Cwd = dir
	} else if base == "" {
		// Build on the caller's work: a helper started from a worktree
		// starts from its branch, not the project's.
		if b, err := proc.Git(dir, "branch", "--show-current"); err == nil {
			req.Base = strings.TrimSpace(b)
		}
	} else {
		req.Base = base
	}
	sess, err := (&agents.AgentService{}).Launch(req)
	if err != nil {
		return AgentRow{}, err
	}
	prompted.Store(sess.ID, time.Now())
	return agentRow(sess), nil
}

// childMeta marks an agent started through the API with the agent that
// started it and how deep in the chain it is, refusing past maxDepth.
func childMeta(self *terminal.SessionInfo) (map[string]string, error) {
	depth := 1
	meta := map[string]string{}
	if self != nil {
		if self.Kind == "agent" {
			d, _ := strconv.Atoi(self.Meta["depth"])
			depth = d + 1
		}
		meta["parent"] = self.ID
	}
	if depth > maxDepth {
		return nil, fmt.Errorf("this agent was itself started by an agent: agents can start agents only %d levels deep", maxDepth)
	}
	meta["depth"] = strconv.Itoa(depth)
	return meta, nil
}

func defaultAgent(agent string) string {
	return proc.FirstNonEmpty(agent, store.CurrentSettings().DefaultAgent, "claude")
}

func apiAgentSend(id, text string) error {
	s, err := findAgent(id)
	if err != nil {
		return err
	}
	if !s.Running {
		return fmt.Errorf("agent %s has exited", s.ID)
	}
	// The answer acknowledges the bell that asked for it, as looking at the
	// agent in the UI does; a bell rung after it is a new question.
	terminal.Sessions.ClearBell(s.ID)
	if err := (&agents.AgentService{}).Send(s.ID, text); err != nil {
		return err
	}
	prompted.Store(s.ID, time.Now())
	return nil
}

// AgentReport is an agent's status with what its screen shows.
type AgentReport struct {
	AgentRow
	// Code is the agent program's exit code, once it exited.
	Code     int    `json:"code,omitempty"`
	TimedOut bool   `json:"timedOut,omitempty"`
	Seconds  int    `json:"seconds,omitempty"`
	Screen   string `json:"screen"`
}

func (r AgentReport) Text() string {
	head := fmt.Sprintf("%s · %s · %s", r.Agent, r.Title, r.Status)
	switch {
	case r.TimedOut:
		head += fmt.Sprintf(" (still %s after %ds)", r.Status, r.Seconds)
	case r.Status == "exited":
		head += fmt.Sprintf(" (exit code %d)", r.Code)
	}
	if r.Branch != "" {
		head += " · " + r.Branch
	}
	return head + "\n\n" + r.Screen + "\n"
}

// ExitCode is the CLI's: 0 when the agent is done, 2 when it needs
// attention (a question, a permission), 1 when it crashed or the wait
// timed out.
func (r AgentReport) ExitCode() int {
	switch {
	case r.TimedOut, r.Status == "exited" && r.Code != 0:
		return 1
	case r.Status == "attention":
		return 2
	}
	return 0
}

func report(s terminal.SessionInfo, lines int) AgentReport {
	if lines <= 0 {
		lines = 40
	}
	screen, err := terminal.Screen(s.ID)
	if err != nil {
		screen = "(" + err.Error() + ")"
	}
	r := AgentReport{AgentRow: agentRow(s), Screen: lastLines(screen, lines)}
	if !s.Running {
		r.Code = s.ExitCode
	}
	// A question on screen doesn't ring the bell: an agent asking whether
	// to trust a new worktree looks idle, but it is waiting for an answer.
	if r.Status == "idle" && asks(screen) {
		r.Status = "attention"
	}
	return r
}

// question matches what agents show when they wait for an answer: a
// confirmation (Claude Code's "Enter to confirm · Esc to cancel",
// Antigravity's "enter Confirm"), a trust prompt, a yes/no.
var question = regexp.MustCompile(`(?i)enter to confirm|enter confirm|do you trust|trust this folder|\((y/n|yes/no)\)|\[y/n\]|allow (this|the) [^\n]*\?|do you want to (proceed|continue|allow|make)|esc to cancel`)

// asks reports whether a screen ends on a question. Agents draw it at the
// bottom, so only the last lines count: once answered, it scrolls up into
// the conversation.
func asks(screen string) bool {
	var tail []string
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0 && len(tail) < 5; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			tail = append(tail, lines[i])
		}
	}
	return question.MatchString(strings.Join(tail, "\n"))
}

func apiAgentStatus(id string, lines int) (AgentReport, error) {
	s, err := findAgent(id)
	if err != nil {
		return AgentReport{}, err
	}
	return report(s, lines), nil
}

func apiAgentWait(c api.Caller, id string, timeout, lines int) (AgentReport, error) {
	s, err := findAgent(id)
	if err != nil {
		return AgentReport{}, err
	}
	if c.Session == s.ID {
		return AgentReport{}, errors.New("an agent can't wait for itself")
	}
	limit := time.Duration(timeout) * time.Second
	if timeout <= 0 {
		limit = 30 * time.Minute
	}
	start := time.Now()
	deadline := time.After(limit)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		s, err = findAgent(s.ID)
		if err != nil {
			return AgentReport{}, errors.New("the agent's terminal was closed")
		}
		if done(s, time.Now()) {
			return report(s, lines), nil
		}
		select {
		case <-c.Ctx.Done():
			return AgentReport{}, errors.New("stopped waiting")
		case <-deadline:
			r := report(s, lines)
			r.TimedOut, r.Seconds = true, int(time.Since(start).Seconds())
			return r, nil
		case <-tick.C:
		}
	}
}

// done reports whether wait can stop: the agent exited, needs attention,
// or has been quiet for a while since its last task.
func done(s terminal.SessionInfo, now time.Time) bool {
	switch agents.Status(s, now) {
	case "exited", "attention":
		return true
	case "idle":
		last := time.UnixMilli(s.LastActivity)
		if t, ok := prompted.Load(s.ID); ok {
			// Given a task through the API: it has to have worked on it, or
			// to have stayed quiet past the grace period.
			if at := t.(time.Time); !last.After(at) && now.Sub(at) < startGrace {
				return false
			}
		}
		return now.Sub(last) >= quietFor
	}
	return false
}
