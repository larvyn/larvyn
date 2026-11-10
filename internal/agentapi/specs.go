package agentapi

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"larvyn/internal/agents"
	"larvyn/internal/api"
	"larvyn/internal/terminal"
)

// The Elephant-Goldfish workflow through the API: an agent (or the user in a
// terminal) can have a spec drafted, reviewed cold, built in worktrees and
// audited for drift, as the Agents view does. See internal/agents/egm.go.
func init() {
	api.Register("specs.list", "List the project's specs (.larvyn/specs) with their review status", apiSpecList)
	api.Register("specs.show", "Print a spec, or its latest review", apiSpecShow,
		"slug: the spec (specs list shows them)",
		"review?: print its review instead")
	api.Register("specs.draft", "Start an Elephant that explores the code and writes a spec for a feature, writing no code", apiSpecDraft,
		"prompt: the feature to specify",
		"agent?: which agent; the default agent of Settings when left out",
		"slug?: work on this existing spec instead of a new one")
	api.Register("specs.review", "Start a Goldfish, a fresh agent with no memory, that reviews a spec cold and writes its verdict", apiSpecReview,
		"slug: the spec",
		"agent?: which agent; ideally not the one that wrote the spec",
		"focus?: what to look at especially")
	api.Register("specs.revise", "Hand a spec's latest review back to its Elephant (or a new one) to fold in", apiSpecRevise,
		"slug: the spec",
		"agent?: the agent for a new Elephant, when none is working on the spec")
	api.Register("specs.build", "Start builders on a spec, each in its own worktree; the spec needs a READY review of its current version unless --force", apiSpecBuild,
		"slug: the spec",
		"agents?: comma-separated agents, the default agent when left out",
		"notes?: extra instructions for the builders",
		"force?: build it without an approved review")
	api.Register("specs.drift", "Start a Goldfish auditor that compares a builder's changes with its spec and writes a drift report", apiSpecDrift,
		"id: the builder agent's terminal",
		"agent?: which agent audits; ideally not the builder's")
	api.Register("specs.fix", "Send a builder its drift report to work through", apiSpecFix,
		"id: the builder agent's terminal")
	api.MarkReadOnly("specs.list", "specs.show")
}

// SpecRow is a line of `larvyn specs list`.
type SpecRow struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Rounds  int    `json:"rounds"`
	Path    string `json:"path"`
	Changed string `json:"changed"`
}

// specStatus is a spec's place in the workflow, in a word or two.
func specStatus(sf agents.SpecFile) string {
	switch {
	case sf.Approved:
		return "approved"
	case !sf.HasReview:
		return "draft"
	case sf.Stale:
		return "revised, needs review"
	case sf.Verdict == "":
		return "reviewed, no verdict"
	}
	return strings.ToLower(sf.Verdict)
}

func callerSpecs(c api.Caller) (projectID string, self *terminal.SessionInfo, err error) {
	p, _, self, err := callerProject(c)
	return p.ID, self, err
}

func apiSpecList(c api.Caller) ([]SpecRow, error) {
	id, _, err := callerSpecs(c)
	if err != nil {
		return nil, err
	}
	list, err := (&agents.AgentService{}).Specs(id)
	if err != nil {
		return nil, err
	}
	out := []SpecRow{}
	for _, sf := range list {
		out = append(out, SpecRow{Slug: sf.Slug, Title: sf.Title, Status: specStatus(sf), Rounds: sf.Rounds, Path: sf.Path,
			Changed: time.UnixMilli(max(sf.UpdatedAt, sf.ReviewAt)).Format(time.DateTime)})
	}
	return out, nil
}

// SpecText is a spec or review as `larvyn specs show` prints it.
type SpecText struct {
	Slug    string `json:"slug"`
	Status  string `json:"status"`
	Verdict string `json:"verdict,omitempty"`
	Content string `json:"content"`
}

func (s SpecText) Text() string { return s.Content }

func apiSpecShow(c api.Caller, slug string, review bool) (SpecText, error) {
	id, _, err := callerSpecs(c)
	if err != nil {
		return SpecText{}, err
	}
	sf, err := (&agents.AgentService{}).ReadSpec(id, slug)
	if err != nil {
		return SpecText{}, fmt.Errorf("spec %q not found (larvyn specs list shows them)", slug)
	}
	out := SpecText{Slug: sf.Slug, Status: specStatus(sf), Verdict: sf.Verdict, Content: sf.Content}
	if review {
		if !sf.HasReview {
			return SpecText{}, errors.New("this spec has no review yet: larvyn specs review " + sf.Slug)
		}
		out.Content = sf.Review
	}
	return out, nil
}

// started records that the API gave an agent its task, for agents wait.
func started(s terminal.SessionInfo, err error) (AgentRow, error) {
	if err != nil {
		return AgentRow{}, err
	}
	prompted.Store(s.ID, time.Now())
	return agentRow(s), nil
}

func apiSpecDraft(c api.Caller, prompt, agent, slug string) (AgentRow, error) {
	id, self, err := callerSpecs(c)
	if err != nil {
		return AgentRow{}, err
	}
	meta, err := childMeta(self)
	if err != nil {
		return AgentRow{}, err
	}
	return started((&agents.AgentService{}).LaunchElephant(agents.ElephantRequest{ProjectID: id, Agent: defaultAgent(agent), Prompt: prompt, Slug: slug, Meta: meta}))
}

func apiSpecReview(c api.Caller, slug, agent, focus string) (AgentRow, error) {
	id, self, err := callerSpecs(c)
	if err != nil {
		return AgentRow{}, err
	}
	meta, err := childMeta(self)
	if err != nil {
		return AgentRow{}, err
	}
	return started((&agents.AgentService{}).LaunchGoldfish(agents.GoldfishRequest{ProjectID: id, Agent: defaultAgent(agent), Slug: slug, Focus: focus, Meta: meta}))
}

func apiSpecRevise(c api.Caller, slug, agent string) (AgentRow, error) {
	id, self, err := callerSpecs(c)
	if err != nil {
		return AgentRow{}, err
	}
	// Revising can start a new Elephant: it counts toward the depth limit
	// like any agent an agent starts.
	meta, err := childMeta(self)
	if err != nil {
		return AgentRow{}, err
	}
	return started(agents.ReviseSpecWithMeta(id, slug, defaultAgent(agent), meta))
}

func apiSpecBuild(c api.Caller, slug, list, notes string, force bool) ([]AgentRow, error) {
	id, self, err := callerSpecs(c)
	if err != nil {
		return nil, err
	}
	meta, err := childMeta(self)
	if err != nil {
		return nil, err
	}
	var kinds []string
	for _, k := range strings.Split(list, ",") {
		if k = strings.TrimSpace(k); k != "" {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		kinds = []string{defaultAgent("")}
	}
	sessions, err := (&agents.AgentService{}).LaunchFromSpec(agents.SpecLaunchRequest{
		ProjectID: id, Slug: slug, Notes: notes, Worktree: true, InstallDeps: true, Force: force, Meta: meta,
	}, kinds)
	out := []AgentRow{}
	for _, s := range sessions {
		prompted.Store(s.ID, time.Now())
		out = append(out, agentRow(s))
	}
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return out, err
}

func apiSpecDrift(c api.Caller, builder, agent string) (AgentRow, error) {
	b, err := findAgent(builder)
	if err != nil {
		return AgentRow{}, err
	}
	if b.Meta["spec"] == "" {
		return AgentRow{}, fmt.Errorf("agent %s is not building a spec", b.ID)
	}
	_, self, _ := callerSpecs(c)
	meta, err := childMeta(self)
	if err != nil {
		return AgentRow{}, err
	}
	return started((&agents.AgentService{}).LaunchDriftCheck(agents.DriftRequest{
		ProjectID: b.ProjectID, Agent: defaultAgent(agent), Slug: b.Meta["spec"], Worktree: b.Cwd, Meta: meta,
	}))
}

func apiSpecFix(builder string) error {
	b, err := findAgent(builder)
	if err != nil {
		return err
	}
	if err := (&agents.AgentService{}).FixDrift(b.ID); err != nil {
		return err
	}
	prompted.Store(b.ID, time.Now())
	return nil
}
