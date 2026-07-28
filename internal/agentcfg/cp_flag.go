//go:build !with_controlplane

package agentcfg

func controlplaneProvidesMgmtTLS() bool { return false }
