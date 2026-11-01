package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

func TestCreateFallbackArgv(t *testing.T) {
	req := CreateRequest{Parent: "/apps", Name: "Acme CRM", Starter: "react", Testing: "pest", DB: "pgsql"}
	argv := createFallbackArgv(req, "acme-crm", []string{"/usr/local/bin/composer"})
	if len(argv) != 3 || argv[0] != store.PHP() {
		t.Fatalf("argv = %v", argv)
	}
	if b, err := os.ReadFile(argv[1]); err != nil || !bytes.Equal(b, createSource) {
		t.Fatalf("create.php not written to %s: %v", argv[1], err)
	}
	raw, err := base64.StdEncoding.DecodeString(argv[2])
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"parent": "/apps", "name": "acme-crm", "starter": "react", "testing": "pest", "db": "pgsql"} {
		if got[k] != want {
			t.Errorf("%s = %v, want %s", k, got[k], want)
		}
	}
	if c, _ := got["composer"].([]any); len(c) != 1 || c[0] != "/usr/local/bin/composer" {
		t.Errorf("composer = %v", got["composer"])
	}
}
