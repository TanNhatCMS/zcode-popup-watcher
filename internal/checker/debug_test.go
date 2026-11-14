package checker

import (
	"strings"
	"testing"
)

func TestDebugParseHHMM(t *testing.T) {
	parts := strings.Split(strings.TrimSpace("17:55"), ":")
	t.Logf("parts=%v len=%d", parts, len(parts))
	a, okA := atoiCheck(parts[0])
	b, okB := atoiCheck(parts[1])
	t.Logf("atoiCheck(17)=%d,%v  atoiCheck(55)=%d,%v", a, okA, b, okB)
	v, ok := parseHHMM("17:55")
	t.Logf("parseHHMM = %d, ok=%v", v, ok)
	t.Logf("atoiCheck trực tiếp 17: %d %v / 55: %d %v", func() int { n, _ := atoiCheck("17"); return n }(), func() bool { _, o := atoiCheck("17"); return o }(), func() int { n, _ := atoiCheck("55"); return n }(), func() bool { _, o := atoiCheck("55"); return o }())
}
