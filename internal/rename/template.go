package rename

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// TemplateFields are the values available to a rename template.
type TemplateFields struct {
	Artist string
	Title  string
	Album  string
	Year   int
}

var illegalChars = regexp.MustCompile(`[/\\:*?"<>|]`)

var placeholderPattern = regexp.MustCompile(`\{(artist|title|album|year)\}`)

// RenderTemplate substitutes placeholders in template with fields' values,
// sanitizing each value for filesystem safety and collapsing any resulting
// empty path segments. The returned path has no extension -- callers
// append the source file's own extension.
func RenderTemplate(template string, fields TemplateFields) (string, error) {
	if strings.TrimSpace(template) == "" {
		return "", fmt.Errorf("empty rename template")
	}

	values := map[string]string{
		"artist": fields.Artist,
		"title":  fields.Title,
		"album":  fields.Album,
	}
	if fields.Year != 0 {
		values["year"] = strconv.Itoa(fields.Year)
	} else {
		values["year"] = ""
	}

	rendered := placeholderPattern.ReplaceAllStringFunc(template, func(match string) string {
		key := match[1 : len(match)-1]
		return sanitize(values[key])
	})

	segments := strings.Split(rendered, "/")
	var kept []string
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg != "" {
			kept = append(kept, seg)
		}
	}
	if len(kept) == 0 {
		return "", fmt.Errorf("rendered template is empty for fields %+v", fields)
	}
	return filepath.Join(kept...), nil
}

func sanitize(value string) string {
	return strings.TrimSpace(illegalChars.ReplaceAllString(value, ""))
}
