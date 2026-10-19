package main

import "testing"

func TestBuildVersion(t *testing.T) {
	old := version
	defer func() { version = old }()
	version = "1.2.3"
	if got := buildVersion(); got != "1.2.3" {
		t.Fatalf("stamped version %q", got)
	}
	version = ""
	if got := buildVersion(); got == "" {
		t.Fatal("empty version")
	}
}
