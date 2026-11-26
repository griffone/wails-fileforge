package automation

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"fileforge-desktop/internal/outputname"
	"fileforge-desktop/internal/platform/fsx"
)

// organizeTokens lists every token a folder or name template may use. The list
// is also part of the save-time error message, so a user sees what exists.
var organizeTokens = []string{"name", "ext", "year", "month", "date", "title"}

const (
	// organizeTokenSuffix is appended to a title source in the error details.
	organizeTitleToolID = "automation.organize"

	// maxRenderedFolderSegments bounds a rendered folder template so a title
	// with many separators can never build an extremely deep tree.
	maxRenderedFolderSegments = 16
)

// unknownOrganizeTokenError reports a template token the rule cannot resolve.
// Save rejects it before the rule is stored; rendering re-checks so a
// hand-edited store fails with the same explanation.
type unknownOrganizeTokenError struct {
	token string
}

func (e *unknownOrganizeTokenError) Error() string {
	return fmt.Sprintf(
		"The template uses the unknown token '{%s}'. Available tokens: %s.",
		e.token,
		strings.Join(organizeTokenList(), ", "),
	)
}

func organizeTokenList() []string {
	quoted := make([]string, 0, len(organizeTokens))
	for _, token := range organizeTokens {
		quoted = append(quoted, "{"+token+"}")
	}

	return quoted
}

// templatePart is one parsed literal or token of an organize template.
type templatePart struct {
	literal string
	token   string
}

// parseOrganizeTemplate splits a template into literal and token parts. It is
// the only token parser: save-time validation and rendering both call it, so a
// token that validates can always render. A stray '}' is a template error
// instead of being kept as literal text.
func parseOrganizeTemplate(template string) ([]templatePart, error) {
	parts := make([]templatePart, 0, 4)
	var literal strings.Builder

	flushLiteral := func() {
		if literal.Len() == 0 {
			return
		}
		parts = append(parts, templatePart{literal: literal.String()})
		literal.Reset()
	}

	for index := 0; index < len(template); {
		switch template[index] {
		case '{':
			end := strings.IndexByte(template[index:], '}')
			if end < 0 {
				return nil, fmt.Errorf("The template has a '{' without a matching '}'.")
			}
			token := template[index+1 : index+end]
			if !knownOrganizeToken(token) {
				return nil, &unknownOrganizeTokenError{token: token}
			}
			flushLiteral()
			parts = append(parts, templatePart{token: token})
			index += end + 1
		case '}':
			return nil, fmt.Errorf("The template has a '}' without a matching '{'.")
		default:
			literal.WriteByte(template[index])
			index++
		}
	}
	flushLiteral()

	return parts, nil
}

func knownOrganizeToken(token string) bool {
	for _, candidate := range organizeTokens {
		if candidate == token {
			return true
		}
	}

	return false
}

// organizeFileMeta holds the values a template can interpolate for one file.
type organizeFileMeta struct {
	name  string
	ext   string
	year  string
	month string
	date  string
	title string
}

func (m organizeFileMeta) tokenValue(token string) (string, bool) {
	switch token {
	case "name":
		return m.name, true
	case "ext":
		return m.ext, true
	case "year":
		return m.year, true
	case "month":
		return m.month, true
	case "date":
		return m.date, true
	case "title":
		if strings.TrimSpace(m.title) != "" {
			return m.title, true
		}

		return m.name, true
	}

	return "", false
}

func renderOrganizeTemplate(parts []templatePart, meta organizeFileMeta) (string, error) {
	var builder strings.Builder
	for _, part := range parts {
		if part.token == "" {
			builder.WriteString(part.literal)
			continue
		}
		value, ok := meta.tokenValue(part.token)
		if !ok {
			return "", &unknownOrganizeTokenError{token: part.token}
		}
		builder.WriteString(value)
	}

	return builder.String(), nil
}

// organizeMetaFor derives the template values from a file path and its
// modification time. {date} is YYYY-MM-DD, {year} is YYYY and {month} is the
// two-digit MM, so "{year}/{month}" builds a stable monthly tree.
func organizeMetaFor(path string, modified time.Time, title string) organizeFileMeta {
	base := filepath.Base(path)
	extension := filepath.Ext(base)
	name := strings.TrimSuffix(base, extension)
	if strings.TrimSpace(name) == "" {
		name = base
	}

	return organizeFileMeta{
		name:  name,
		ext:   extension,
		year:  modified.Format("2006"),
		month: modified.Format("01"),
		date:  modified.Format("2006-01-02"),
		title: title,
	}
}

// validateOrganizeTemplates rejects a template that could never render safely:
// an unknown token, an absolute or traversing folder path, or a name template
// that tries to create folders. Every rule is checked at save time, so the
// watcher never discovers this per file.
func validateOrganizeTemplates(folderTemplate, nameTemplate string) error {
	if _, err := parseOrganizeTemplate(folderTemplate); err != nil {
		return err
	}
	if _, err := parseOrganizeTemplate(nameTemplate); err != nil {
		return err
	}

	if trimmed := strings.TrimSpace(folderTemplate); trimmed != "" {
		if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, `\`) {
			return fmt.Errorf("The folder template must be relative to the target folder, not absolute.")
		}
		for _, segment := range splitTemplateSegments(folderTemplate) {
			trimmedSegment := strings.TrimSpace(segment)
			switch {
			case trimmedSegment == "":
				return fmt.Errorf("The folder template has an empty folder segment; remove the repeated or trailing separator.")
			case trimmedSegment == "." || trimmedSegment == "..":
				return fmt.Errorf("The folder template cannot contain '.' or '..' segments.")
			case strings.Contains(trimmedSegment, ":"):
				return fmt.Errorf("The folder template cannot contain ':'.")
			}
		}
	}

	if strings.ContainsAny(nameTemplate, `/\`) {
		return fmt.Errorf("The name template cannot contain a folder separator; the folder template builds folders.")
	}
	if strings.TrimSpace(nameTemplate) == ".." || strings.TrimSpace(nameTemplate) == "." {
		return fmt.Errorf("The name template cannot be '.' or '..'.")
	}

	return nil
}

// splitTemplateSegments splits a folder template on either separator so the
// same rule works on Windows and POSIX.
func splitTemplateSegments(template string) []string {
	normalized := strings.NewReplacer(`\`, "/").Replace(template)

	return strings.Split(normalized, "/")
}

// renderOrganizeDestination resolves the destination path for one file inside
// targetDir. A blank folder template writes directly into targetDir; a blank
// name template keeps the original base name. Rendered folder segments and a
// rendered name are sanitized into single path segments, and the final path is
// checked to stay inside targetDir.
func renderOrganizeDestination(targetDir, folderTemplate, nameTemplate string, meta organizeFileMeta) (string, error) {
	destination := targetDir

	if trimmed := strings.TrimSpace(folderTemplate); trimmed != "" {
		parts, err := parseOrganizeTemplate(trimmed)
		if err != nil {
			return "", err
		}
		rendered, err := renderOrganizeTemplate(parts, meta)
		if err != nil {
			return "", err
		}

		segments := splitTemplateSegments(rendered)
		if len(segments) > maxRenderedFolderSegments {
			return "", fmt.Errorf("The folder template rendered too many folder levels.")
		}
		for _, segment := range segments {
			safe := fsx.SanitizeBaseName(segment)
			if safe == "" {
				return "", fmt.Errorf("The folder template rendered an empty folder name.")
			}
			destination = filepath.Join(destination, safe)
		}
	}

	name := meta.name + meta.ext
	if trimmed := strings.TrimSpace(nameTemplate); trimmed != "" {
		parts, err := parseOrganizeTemplate(trimmed)
		if err != nil {
			return "", err
		}
		rendered, err := renderOrganizeTemplate(parts, meta)
		if err != nil {
			return "", err
		}
		safe := fsx.SanitizeBaseName(rendered)
		if safe == "" {
			return "", fmt.Errorf("The name template rendered an empty file name.")
		}
		name = safe
	}

	destination = filepath.Join(destination, name)
	if !pathWithin(targetDir, destination) {
		return "", fmt.Errorf("The rendered destination is outside the target folder.")
	}

	return destination, nil
}

// defaultOrganizeTitle resolves the metadata title used by {title}. It reuses
// the same suggestion engine the Pages use; a format without metadata simply
// returns no title and the template falls back to {name}.
func defaultOrganizeTitle(path string) string {
	suggestion, suggestionErr := outputname.Suggest(organizeTitleToolID, path)
	if suggestionErr != nil || !suggestion.Found {
		return ""
	}

	return suggestion.SuggestedName
}
