package main

import (
	"io"
	"strings"
	"testing"
)

func TestReadAdminUsernameLeavesPasswordForTerminalReader(t *testing.T) {
	reader := strings.NewReader("TEST_ONLY_OWNER\nTEST_ONLY_PASSWORD\n")
	username, err := readAdminUsername(reader, "admin")
	remaining, _ := io.ReadAll(reader)
	if err != nil || username != "TEST_ONLY_OWNER" || string(remaining) != "TEST_ONLY_PASSWORD\n" {
		t.Fatal("account reader consumed password input")
	}
	for _, line := range []string{"\n", "\r\n"} {
		if name, err := readAdminUsername(strings.NewReader(line), "TEST_ONLY_EXISTING"); err != nil || name != "TEST_ONLY_EXISTING" {
			t.Fatal("blank initialization reset an existing username")
		}
	}
	for _, line := range []string{" owner\n", "owner\t\n", strings.Repeat("x", 65) + "\n", "no-newline"} {
		if _, err := readAdminUsername(strings.NewReader(line), "admin"); err == nil {
			t.Fatal("invalid account input accepted")
		}
	}
}
