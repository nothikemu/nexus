// Package version exposes build metadata injected at link time.
package version

import (
	"fmt"
	"runtime"
)

// These are overridden via -ldflags "-X github.com/nothikemu/nexus/internal/version.Version=…".
var (
	Version = "0.1.0-dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Info is the structured form of the build metadata.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// Get returns the build metadata for this binary.
func Get() Info {
	return Info{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
}

// String renders a compact one-line version string.
func (i Info) String() string {
	return fmt.Sprintf("nexus %s (%s, %s/%s)", i.Version, short(i.Commit), i.OS, i.Arch)
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}
