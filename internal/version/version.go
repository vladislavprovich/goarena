// Package version records the version of the goarena module. It is
// internal because the version is reported by the module's own tools (the
// benchmark harness writes it into every result document); library users
// should rely on the module version selected by the go command instead.
package version

// Module is the version of the goarena module (semantic versioning, without
// the leading "v").
const Module = "0.3.1"
