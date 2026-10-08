package gateway

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Release builds set Version from the root VERSION file.
var Version = "0.0.4"

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$`)

func compareVersions(a, b string) (int, error) {
	if !versionPattern.MatchString(a) || !versionPattern.MatchString(b) {
		return 0, fmt.Errorf("版本号须为数字形式，例如 0.0.1")
	}
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range aa {
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
