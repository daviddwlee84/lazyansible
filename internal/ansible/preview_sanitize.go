package ansible

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"gopkg.in/yaml.v3"
)

// This deliberately recognizes common key/value diagnostics rather than
// claiming arbitrary Ansible output can be made safe to publish.
var sensitivePair = regexp.MustCompile(`(?i)(["']?[a-z0-9_.-]*(?:password|passwd|token|secret|api[_-]?key|private[_-]?key)[a-z0-9_.-]*["']?\s*[:=]\s*)(\[redacted\]|"(?:\\.|[^"\\])*"|'(?:[^']|'')*'|[^\s,}\]]+)`)
var inputAssignment = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.-]*)=("(?:\\.|[^"\\])*"|'[^']*'|[^\s]+)`)

func cleanTerminalText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = ansi.Strip(text)
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}

func previewSanitizer(request RunRequest) func(string) string {
	secrets := map[string]bool{}
	var collect func(string, any)
	collect = func(key string, value any) {
		if sensitiveObservationKey(key) {
			if text, ok := value.(string); ok && text != "" {
				secrets[text] = true
			}
			return
		}
		switch value := value.(type) {
		case map[string]any:
			for k, v := range value {
				collect(k, v)
			}
		case []any:
			for _, v := range value {
				collect("", v)
			}
		}
	}
	for _, raw := range request.ExtraVars {
		if strings.HasPrefix(raw, "@") {
			continue
		} // Do not reread/decrypt user files to guess secrets.
		var value any
		if json.Unmarshal([]byte(raw), &value) == nil {
			collect("", value)
		} else if strings.Contains(raw, ":") && yaml.Unmarshal([]byte(raw), &value) == nil {
			collect("", value)
		}
		for _, match := range inputAssignment.FindAllStringSubmatch(raw, -1) {
			if sensitiveObservationKey(match[1]) {
				value := strings.Trim(match[2], "\"'")
				if value != "" {
					secrets[value] = true
				}
			}
		}
	}
	for _, entry := range request.Env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && sensitiveObservationKey(key) && value != "" {
			secrets[value] = true
		}
	}
	values := make([]string, 0, len(secrets))
	for value := range secrets {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	replacements := []string{}
	for _, value := range values {
		for _, candidate := range []string{value, cleanTerminalText(value)} {
			if candidate != "" {
				replacements = append(replacements, candidate, "[redacted]")
			}
		}
	}
	replacer := strings.NewReplacer(replacements...)
	return func(text string) string {
		text = cleanTerminalText(text)
		text = replacer.Replace(text)
		text = redact(text)
		// Avoid an expensive broad key matcher on long output that contains no
		// credential-like key at all (the capture limit is four MiB).
		lower := strings.ToLower(text)
		for _, key := range []string{"password", "passwd", "token", "secret", "api_key", "api-key", "apikey", "private_key", "private-key", "privatekey"} {
			if strings.Contains(lower, key) {
				return sensitivePair.ReplaceAllString(text, "${1}[redacted]")
			}
		}
		return text
	}
}
