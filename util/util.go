package util

import (
	"regexp"
)

var SplitMulti = regexp.MustCompile(`[\s,;]+`)
