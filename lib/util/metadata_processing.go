package util

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alessio/shellescape"
)

// Serializes dictionary to usable format
func SerializeMetadata(format, prefix string, data map[string]any) (out []byte, err error) {
	switch format {
	case "json": // Default json
		return json.Marshal(data)
	case "env": // Plain format suitable to use in shell
		m := DotSerialize(prefix, data)
		for key, val := range m {
			line := cleanShellKey(strings.Replace(shellescape.StripUnsafe(key), ".", "_", -1))
			if len(line) == 0 {
				continue
			}
			value := []byte("=" + shellescape.Quote(val) + "\n")
			out = append(out, append(line, value...)...)
		}
	case "ps1": // Plain format suitable to use in powershell
		m := DotSerialize(prefix, data)
		for key, val := range m {
			line := cleanShellKey(strings.Replace(shellescape.StripUnsafe(key), ".", "_", -1))
			if len(line) == 0 {
				continue
			}
			// Shell quote is not applicable here, so using the custom one
			value := []byte("='" + strings.Replace(val, "'", "''", -1) + "'\n")
			out = append(out, append([]byte("$"), append(line, value...)...)...)
		}
	default:
		return out, fmt.Errorf("Unsupported `format`: %s", format)
	}

	return out, nil
}

func cleanShellKey(in string) []byte {
	s := []byte(in)
	j := 0
	for _, b := range s {
		if j == 0 && ('0' <= b && b <= '9') {
			// Skip first numeric symbols
			continue
		}
		if ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z') || ('0' <= b && b <= '9') || b == '_' {
			s[j] = b
			j++
		}
	}
	return s[:j]
}
