package util

import (
	"regexp"
	"time"
)

var StartedAt = time.Now()
var SplitMulti = regexp.MustCompile(`[\s,;]+`)
