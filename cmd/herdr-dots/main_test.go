package main

import (
	"strings"
	"testing"
)

func TestDispatchUnknownCommand(t *testing.T) {
	err := dispatch([]string{"bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("want unknown command error, got %v", err)
	}
}

func TestDispatchNoArgsShowsUsage(t *testing.T) {
	err := dispatch(nil)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("want usage error, got %v", err)
	}
}

func TestDispatchVersion(t *testing.T) {
	if err := dispatch([]string{"version"}); err != nil {
		t.Fatalf("version: %v", err)
	}
}
