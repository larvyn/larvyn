package agents

import (
	"slices"
	"strings"
	"testing"
)

func TestPlaybooks(t *testing.T) {
	ids := map[string]bool{}
	for _, p := range AllPlaybooks() {
		if p.ID == "" || p.Title == "" || p.Text == "" || ids[p.ID] {
			t.Errorf("playbook: %+v", p)
		}
		ids[p.ID] = true
	}
	crud, ok := FindPlaybook("crud")
	if !ok || !slices.Equal(crud.Args, []string{"Model"}) {
		t.Fatalf("crud: %+v", crud)
	}
	text, missing := crud.Fill(map[string]string{"Model": "Invoice"})
	if len(missing) != 0 || !strings.Contains(text, "CRUD for a Invoice resource") {
		t.Errorf("filled: %q %v", text, missing)
	}
	if _, missing := crud.Fill(nil); !slices.Equal(missing, []string{"Model"}) {
		t.Errorf("missing: %v", missing)
	}
	// Blade's {!! !!} is not a placeholder.
	if sec, _ := FindPlaybook("security-review"); len(sec.Args) != 0 {
		t.Errorf("security review args: %v", sec.Args)
	}
}
