package assets

import (
	"embed"
	"strings"
)

//go:embed conf.example.ini version lang/*
var files embed.FS

func Asset(name string) ([]byte, error) { return files.ReadFile(strings.TrimPrefix(name, "assets/")) }
