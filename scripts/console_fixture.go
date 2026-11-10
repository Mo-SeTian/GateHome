//go:build ignore

// Initializes only the disposable browser-test directory supplied by the test runner.
package main

import (
	"io"
	"os"

	"gatehouse/internal/gateway"
)

func main() {
	password, err := io.ReadAll(os.Stdin)
	if err != nil || len(os.Args) != 2 {
		os.Exit(1)
	}
	defer clear(password)
	store, err := gateway.OpenStore(os.Args[1])
	if err != nil {
		os.Exit(1)
	}
	hash, err := gateway.HashPassword(password)
	if err != nil || store.SetAdminAccount("admin", hash) != nil {
		os.Exit(1)
	}
}
