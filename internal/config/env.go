package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var envPattern = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`)

// ExpandEnvStrict expands ${VAR} references and errors if any env var is missing.
func ExpandEnvStrict(input string) (string, error) {
	matches := envPattern.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return input, nil
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(input[last:m[0]])
		name := input[m[2]:m[3]]
		val, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("missing env var %s", name)
		}
		b.WriteString(val)
		last = m[1]
	}
	b.WriteString(input[last:])
	return b.String(), nil
}
