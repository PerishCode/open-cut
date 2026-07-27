package lifecycle

import "github.com/PerishCode/open-cut/lifecycle/process"

// The process-running primitives live in the lifecycle/process leaf so
// consumers that only spawn processes (the renderer closure above all) do not
// inherit installer, signer, data-directory, or cell-broker identity. This
// package keeps the historical lifecycle.* surface as aliases.

type (
	Presentation  = process.Presentation
	Process       = process.Process
	ProcessSpec   = process.ProcessSpec
	Profile       = process.Profile
	SandboxPolicy = process.SandboxPolicy
)

const (
	PresentationHeadless    = process.PresentationHeadless
	PresentationInteractive = process.PresentationInteractive
	ProfileProduction       = process.ProfileProduction
	ProfilePackaged         = process.ProfilePackaged
	ProfileDevelopment      = process.ProfileDevelopment
	ProfileHarness          = process.ProfileHarness
	SandboxDefault          = process.SandboxDefault
	SandboxChromium         = process.SandboxChromium
)

var (
	BootstrapProcess    = process.BootstrapProcess
	ResolvePresentation = process.ResolvePresentation
	Run                 = process.Run
	Start               = process.Start
	VersionedProcess    = process.VersionedProcess
)
