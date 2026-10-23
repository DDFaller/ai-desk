// Package configpaths resolves the operator home separately from a CLI's sandbox HOME.
package configpaths

import (
	"os"
	"strings"
)

func HomeDir() (string, error) {
	if home := os.Getenv("AI_DESK_USER_HOME"); home != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	// ai-desk itself may be launched from a managed CLI session. Its profile and
	// catalog selectors must still see the operator's canonical configuration.
	if index := strings.Index(home, "/.ai-desk/harnesses/"); index > 0 {
		return home[:index], nil
	}
	return home, nil
}
