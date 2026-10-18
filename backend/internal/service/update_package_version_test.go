package service

import "testing"

func TestPackagedVersionComparison(t *testing.T) {
	for _, current := range []string{
		"0.2.17-ent.20261018-1",
		"v0.2.17-ob.20261018-123456789001",
	} {
		for _, latest := range []struct {
			version string
			want    int
		}{
			{"0.2.16", 1},
			{"v0.2.17", 0},
			{"0.2.18", -1},
		} {
			if got := compareVersions(current, latest.version); got != latest.want {
				t.Errorf("打包版本 %s 与上游 %s 比较结果为 %d，期望 %d", current, latest.version, got, latest.want)
			}
		}
	}
}
