// Package version holds Synchro's build-time version, set via -ldflags at build.
package version

// Version is overridden at build time with -ldflags "-X .../version.Version=x.y.z".
var Version = "dev"
