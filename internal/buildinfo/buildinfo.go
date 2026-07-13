package buildinfo

import (
	"os"
	"runtime"
	"runtime/debug"

	"github.com/PerishCode/open-cut/sidecar/protocol"
)

type Info struct {
	Executable      string `json:"executable"`
	GoVersion       string `json:"goVersion"`
	ModuleVersion   string `json:"moduleVersion"`
	VCSRevision     string `json:"vcsRevision,omitempty"`
	VCSModified     bool   `json:"vcsModified"`
	SidecarProtocol string `json:"sidecarProtocol"`
}

func Current() Info {
	executable, _ := os.Executable()
	info := Info{Executable: executable, GoVersion: runtime.Version(), ModuleVersion: "(devel)", SidecarProtocol: protocol.Version}
	if build, ok := debug.ReadBuildInfo(); ok {
		info.ModuleVersion = build.Main.Version
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				info.VCSRevision = setting.Value
			case "vcs.modified":
				info.VCSModified = setting.Value == "true"
			}
		}
	}
	return info
}
