package main

import (
	"os"
	"testing"
)

// Integration tests below read the real music corpus. Pin HOME so they
// never depend on whichever user invokes go test.
func TestMain(m *testing.M) {
	os.Setenv("HOME", "/home/miz")
	os.Exit(m.Run())
}
