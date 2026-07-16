package controller

import (
	"strings"

	"github.com/PerishCode/open-cut/product/command"
)

func commandExtensions(path ...string) map[string]any {
	registry := command.InitialRegistry()
	descriptor, err := registry.Lookup(path)
	if err != nil {
		panic("API command binding is not registered: " + strings.Join(path, " "))
	}
	fingerprint, err := registry.Fingerprint(path)
	if err != nil {
		panic("API command binding is not registered: " + strings.Join(path, " "))
	}
	return map[string]any{
		"x-open-cut-surface":             "creator,agent",
		"x-open-cut-command":             strings.Join(path, " "),
		"x-open-cut-command-fingerprint": fingerprint,
		"x-open-cut-receipt":             descriptor.Receipt,
	}
}
