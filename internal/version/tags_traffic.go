//go:build with_traffic

package version

func init() {
	BuildTags = append(BuildTags, "with_traffic")
}
