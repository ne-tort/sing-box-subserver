//go:build with_controlplane

package version

func init() {
	BuildTags = append(BuildTags, "with_controlplane")
}
