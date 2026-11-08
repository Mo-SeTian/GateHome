package gateway

import (
	"fmt"
	"regexp"
	"strconv"
)

// Release builds set Version from the root VERSION file.
var Version = "0.0.4"

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})(?:-beta\.([1-9][0-9]{0,5}))?$`)

func compareVersions(a, b string) (int, error) {
	aa, bb := versionPattern.FindStringSubmatch(a), versionPattern.FindStringSubmatch(b)
	if aa == nil || bb == nil {
		return 0, fmt.Errorf("版本号须为 0.0.1 或 0.0.1-beta.1 格式")
	}
	for i := 1; i <= 4; i++ {
		if i == 4 && (aa[i] == "" || bb[i] == "") {
			switch {
			case aa[i] == bb[i]:
				return 0, nil
			case aa[i] == "":
				return 1, nil
			default:
				return -1, nil
			}
		}
		x, _ := strconv.Atoi(aa[i])
		y, _ := strconv.Atoi(bb[i])
		if x < y {
			return -1, nil
		}
		if x > y {
			return 1, nil
		}
	}
	return 0, nil
}
