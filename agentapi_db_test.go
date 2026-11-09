package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"larvyn/internal/api"
	"larvyn/internal/project"
)

// LARVYN_TEST_PROJECT=/path/to/a/copy/of/a/laravel/app go test -run TestModelsAndDB -v
// It only reads, and checks that db query can't write.
func TestModelsAndDB(t *testing.T) {
	dir := os.Getenv("LARVYN_TEST_PROJECT")
	if dir == "" {
		t.Skip("LARVYN_TEST_PROJECT not set")
	}
	if _, err := (&project.ProjectService{}).Add(dir); err != nil {
		t.Fatal(err)
	}
	c := api.Caller{Cwd: dir}

	r := command(c, "models")
	var models Models
	json.Unmarshal(r.Result, &models)
	if r.Error != "" || len(models) == 0 {
		t.Fatalf("models: %+v", r)
	}
	t.Logf("models:\n%s", r.Text)

	r = command(c, "db", "tables")
	var tables DBTables
	json.Unmarshal(r.Result, &tables)
	if r.Error != "" || len(tables.Tables) == 0 {
		t.Fatalf("db tables: %+v", r)
	}
	t.Logf("db tables:\n%s", r.Text)
	// A model's table that exists: a model can be ahead of its migration.
	var table string
	for _, m := range models {
		for _, tb := range tables.Tables {
			if table == "" && tb.Name == m.Table {
				table = m.Table
			}
		}
	}
	if table == "" {
		t.Fatal("no model has a table")
	}

	r = command(c, "db", "table", table)
	if r.Error != "" || !strings.Contains(r.Text, "id") {
		t.Fatalf("db table %s: %+v", table, r)
	}
	t.Logf("db table:\n%s", r.Text)
	if r := command(c, "db", "table", "no_such_table"); r.Error == "" {
		t.Errorf("a missing table: %+v", r)
	}

	r = command(c, "db", "query", "select * from "+table, "--limit", "2")
	if r.Error != "" {
		t.Fatalf("db query: %+v", r)
	}
	t.Logf("db query:\n%s", r.Text)

	before := command(c, "db", "query", "select count(*) as n from "+table).Text
	for _, sql := range []string{
		"delete from " + table,
		"with x as (select 1) delete from " + table,
		"select 1; delete from " + table,
	} {
		if r := command(c, "db", "query", sql); r.Error == "" {
			t.Errorf("%q ran: %+v", sql, r)
		} else {
			t.Logf("%q refused: %s", sql, r.Error)
		}
	}
	if after := command(c, "db", "query", "select count(*) as n from "+table).Text; after != before {
		t.Errorf("rows changed: %q → %q", before, after)
	}
}
