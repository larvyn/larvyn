package system

import (
	"net/url"
	"strings"
	"testing"

	"larvyn/internal/updater"
)

func TestBugReportURL(t *testing.T) {
	u, err := url.Parse(bugReportURL("boom\n" + strings.Repeat("x", 5000)))
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Scheme + "://" + u.Host + u.Path; got != feedbackRepo+"/issues/new" {
		t.Fatalf("base = %s", got)
	}
	q := u.Query()
	if q.Get("template") != "bug_report.yml" || q.Get("version") != updater.Version || q.Get("os") == "" {
		t.Fatalf("missing prefilled fields: %v", q)
	}
	if wh := q.Get("what-happened"); !strings.HasPrefix(wh, "```\nboom") || len(wh) > 1600 {
		t.Fatalf("details not trimmed: %d bytes", len(wh))
	}
	if len(u.String()) > 8000 {
		t.Fatalf("url too long: %d", len(u.String()))
	}

	if q := mustQuery(t, bugReportURL("")); q.Has("what-happened") {
		t.Fatal("empty details should not prefill what-happened")
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}
