package conv

import (
	"strconv"
	"strings"
)

// FormatFloat formats float val as a string.
func FormatFloat(val float64) string {
	str := strconv.FormatFloat(val, 'f', 5, 64)
	return strings.TrimRight(strings.TrimRight(str, "0"), ".")
}
