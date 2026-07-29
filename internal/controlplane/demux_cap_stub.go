//go:build with_controlplane && !with_demux

package controlplane

// demuxInBinary is false when demux was not compiled into this binary.
const demuxInBinary = false
