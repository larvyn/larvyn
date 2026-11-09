package agents

import (
	"regexp"
	"strings"
)

// Playbook is a ready-made Laravel task for an agent: the Agents view offers
// them, and `larvyn mcp` serves them as MCP prompts. A {Name} in the text is
// filled in by whoever uses it (the MCP prompt's arguments).
type Playbook struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
	// Spec playbooks are meant for the spec-first workflow.
	Spec bool `json:"spec,omitempty"`
	// Args are the {Name} placeholders in Text, in order.
	Args []string `json:"args,omitempty"`
}

var playbooks = []Playbook{
	{ID: "plan-feature", Title: "Plan a feature first", Spec: true,
		Text: "Specify {Feature}: the data model and migrations, the routes and validation, the policies, the edge cases and the Pest tests, following the patterns already in this codebase."},
	{ID: "crud", Title: "CRUD resource",
		Text: "Build a complete CRUD for a {Model} resource: migration, model with casts and relationships, factory, seeder, Form Requests for validation, a policy, a resource controller, routes and Pest feature tests. Follow the conventions already used in this codebase."},
	{ID: "write-tests", Title: "Write tests",
		Text: "Find the untested controllers, actions and jobs in app/ and write Pest feature and unit tests for them. Use factories, cover the happy path and the validation failures, and run php artisan test until everything passes."},
	{ID: "fix-tests", Title: "Fix failing tests",
		Text: "Run php artisan test, investigate every failure, and fix the root cause in the application code (not the tests, unless a test is clearly wrong). Re-run until green and summarise what was broken."},
	{ID: "n-plus-one", Title: "Find N+1 queries",
		Text: "Audit controllers, Livewire components and API resources for N+1 query problems and missing indexes. Add eager loading and migrations for indexes where needed, and explain each change."},
	{ID: "security-review", Title: "Security review",
		Text: "Review this Laravel app for security problems: mass assignment, missing authorization (policies/gates), unvalidated input, raw SQL, XSS in Blade ({!! !!}), CSRF exemptions, exposed secrets and insecure config. Fix what you can and list the rest."},
	{ID: "upgrade-laravel", Title: "Upgrade Laravel",
		Text: "Upgrade this application to the latest Laravel major version following the official upgrade guide: update composer.json constraints, apply the breaking changes, migrate to the slim application structure if appropriate, and make sure the test suite passes."},
	{ID: "refactor-actions", Title: "Refactor to actions",
		Text: "Move business logic out of fat controllers into single-purpose Action classes in app/Actions, keep controllers thin, add type hints and return types, and keep behaviour identical (tests must still pass)."},
	{ID: "api-endpoints", Title: "API endpoints",
		Text: "Add a versioned JSON API (routes/api.php, /api/v1) for the main models with API Resources, Form Request validation, Sanctum authentication, pagination and filtering, plus Pest tests for every endpoint."},
}

// placeholder is a {Name} to fill in; {!! !!} in the security review is
// Blade, not one.
var placeholder = regexp.MustCompile(`\{([A-Z][A-Za-z]*)\}`)

func init() {
	for i := range playbooks {
		for _, m := range placeholder.FindAllStringSubmatch(playbooks[i].Text, -1) {
			playbooks[i].Args = append(playbooks[i].Args, m[1])
		}
	}
}

// Playbooks lists the playbooks, for the Agents view.
func (a *AgentService) Playbooks() []Playbook { return AllPlaybooks() }

// AllPlaybooks is Playbooks for the local API: a function rather than a
// method so Wails does not bind it twice.
func AllPlaybooks() []Playbook {
	out := make([]Playbook, len(playbooks))
	copy(out, playbooks)
	return out
}

// FindPlaybook returns the playbook with this id, or false.
func FindPlaybook(id string) (Playbook, bool) {
	for _, p := range playbooks {
		if p.ID == strings.TrimSpace(id) {
			return p, true
		}
	}
	return Playbook{}, false
}

// Fill puts values in a playbook's placeholders and lists the ones left
// without a value.
func (p Playbook) Fill(values map[string]string) (text string, missing []string) {
	text = placeholder.ReplaceAllStringFunc(p.Text, func(m string) string {
		name := m[1 : len(m)-1]
		if v := strings.TrimSpace(values[name]); v != "" {
			return v
		}
		missing = append(missing, name)
		return m
	})
	return text, missing
}
