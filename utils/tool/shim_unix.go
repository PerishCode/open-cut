//go:build !windows

package tool

import "path/filepath"

func repositoryShimCommand(repositoryRoot, name string) (Command, error) {
	command := Command{Executable: filepath.Join(repositoryRoot, ".oc-control", "bin", name)}
	if err := validateCommand(name, command); err != nil {
		return Command{}, err
	}
	return command, nil
}
