module github.com/ne-tort/sing-box-subserver

go 1.26

toolchain go1.26.0

require github.com/sagernet/sing-box v1.14.0-lx.17

require (
	github.com/sagernet/sing v0.8.12-0.20260715103206-ac5f044167e4 // indirect
	golang.org/x/sys v0.43.0 // indirect
)

replace github.com/sagernet/sing-box => ./third_party/sing-box-lx

replace github.com/sagernet/wireguard-go => ./third_party/sing-box-lx/submodules/wireguard-go
