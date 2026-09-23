// Package buildinfo reports the same build identity from the CLI and TUI.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

var Version = "dev"
var Commit = ""
var Date = ""

func resolvedVersion(injected, module string) string {
	if injected != "" && injected != "dev" && injected != "(devel)" {
		return injected
	}
	if module != "" && module != "(devel)" {
		return module
	}
	return "dev"
}

func String() string {
	module := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		module = info.Main.Version
		// Go 1.24 may synthesize a pseudo-version for a local checkout. A
		// source build remains a development build; module installs retain
		// their published version (including meaningful pseudo-versions).
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				module = ""
				break
			}
		}
	}
	v := resolvedVersion(Version, module)
	if v != "dev" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}
