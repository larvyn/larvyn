package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLitePath(t *testing.T) {
	root := filepath.FromSlash("/apps/acme")
	abs := filepath.FromSlash("/data/acme.sqlite")
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"DB_CONNECTION": "sqlite"}, filepath.Join(root, "database", "database.sqlite")},
		{map[string]string{"DB_CONNECTION": "SQLite", "DB_DATABASE": abs}, abs},
		{map[string]string{"DB_CONNECTION": "sqlite", "DB_DATABASE": "storage/app.sqlite"}, filepath.Join(root, "storage", "app.sqlite")},
		{map[string]string{"DB_CONNECTION": "sqlite", "DB_DATABASE": ":memory:"}, ""},
		{map[string]string{"DB_CONNECTION": "mysql", "DB_DATABASE": "acme"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := sqlitePath(root, c.env); got != c.want {
			t.Errorf("sqlitePath(%v) = %q, want %q", c.env, got, c.want)
		}
	}
}

func TestEnsureSQLite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=sqlite\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &ProjectService{}
	p, err := svc.Add(root)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Remove(p.ID)

	created, err := svc.EnsureSQLite(p.ID)
	if err != nil || !created {
		t.Fatalf("first call: created=%v err=%v, want a new file", created, err)
	}
	if _, err := os.Stat(filepath.Join(root, "database", "database.sqlite")); err != nil {
		t.Fatalf("database file missing: %v", err)
	}
	created, err = svc.EnsureSQLite(p.ID)
	if err != nil || created {
		t.Fatalf("second call: created=%v err=%v, want no-op", created, err)
	}
}
