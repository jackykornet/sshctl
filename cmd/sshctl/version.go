package main

import "runtime/debug"

// version is the release tag, set at build time by the release workflow
// with -ldflags "-X main.version=<tag>".
var version string

// buildVersion is what --version prints: the release tag, or the module
// version Go recorded (go install), or "dev" for a local build.
func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
