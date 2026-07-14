//go:build windows

package tool

import "path/filepath"

func repositoryShimCommand(repositoryRoot, name string) (Command, error) {
	script := filepath.Join(repositoryRoot, ".oc-control", "bin", name+".cmd")
	if err := validateCommand(name+" shim", Command{Executable: script}); err != nil {
		return Command{}, err
	}
	commandInterpreter, err := Resolve("cmd.exe")
	if err != nil {
		return Command{}, err
	}
	return Command{Executable: commandInterpreter, Prefix: []string{"/d", "/s", "/c", script}}, nil
}
