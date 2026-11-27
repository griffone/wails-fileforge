package engine

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// DOCXTitles carries the two naming sources available without rendering a
// DOCX package: the core-properties title and the first styled heading.
type DOCXTitles struct {
	CoreTitle string
	Heading   string
}

// ReadDOCXTitles reads docProps/core.xml and, only when the core title is
// missing, the first heading in word/document.xml. A malformed package, an
// unreadable entry or invalid XML is reported as an error; a missing title is
// not.
func ReadDOCXTitles(reader io.ReaderAt, size int64) (DOCXTitles, error) {
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return DOCXTitles{}, err
	}

	var titles DOCXTitles
	if entry := findZipEntry(archive, "docProps/core.xml"); entry != nil {
		coreTitle, readErr := readDOCXCoreTitle(entry)
		if readErr != nil {
			return DOCXTitles{}, readErr
		}
		titles.CoreTitle = coreTitle
	}

	if strings.TrimSpace(titles.CoreTitle) != "" {
		return titles, nil
	}

	entry := findZipEntry(archive, "word/document.xml")
	if entry == nil {
		return DOCXTitles{}, fmt.Errorf("docx package has no word/document.xml")
	}

	heading, readErr := readDOCXFirstHeading(entry)
	if readErr != nil {
		return DOCXTitles{}, readErr
	}
	titles.Heading = heading

	return titles, nil
}

// ReadMarkdownTitle returns the text of the first level-1 heading. Parsing
// reuses the same goldmark configuration as the Markdown renderer, so fenced
// code and other block syntax behave the same way as the conversion itself.
func ReadMarkdownTitle(source []byte) (string, error) {
	parser := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()
	document := parser.Parse(text.NewReader(source))

	title := ""
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		heading, ok := node.(*ast.Heading)
		if !ok || heading.Level != 1 {
			return ast.WalkContinue, nil
		}

		title = markdownNodeText(heading, source)

		return ast.WalkStop, nil
	})

	return strings.TrimSpace(title), nil
}

func findZipEntry(archive *zip.Reader, name string) *zip.File {
	for _, entry := range archive.File {
		if entry.Name == name {
			return entry
		}
	}

	return nil
}

func readDOCXCoreTitle(entry *zip.File) (string, error) {
	reader, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()

	var core struct {
		Title string `xml:"title"`
	}
	if decodeErr := xml.NewDecoder(reader).Decode(&core); decodeErr != nil {
		return "", decodeErr
	}

	return strings.TrimSpace(core.Title), nil
}

// readDOCXFirstHeading returns the text of the first paragraph styled as a
// heading (Title or Heading1..Heading6). Nested paragraphs, such as those in
// text boxes, are ignored so a caption never wins over the document heading.
func readDOCXFirstHeading(entry *zip.File) (string, error) {
	reader, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()

	return parseDOCXFirstHeading(reader)
}

func parseDOCXFirstHeading(source io.Reader) (string, error) {
	decoder := xml.NewDecoder(source)

	depth := 0
	style := ""
	var paragraph strings.Builder
	capturingText := false

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return "", nil
		}
		if err != nil {
			return "", err
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "p":
				if depth == 0 {
					style = ""
					paragraph.Reset()
					capturingText = false
				}
				depth++
			case "pStyle":
				if depth == 1 {
					style = attributeValue(element, "val")
				}
			case "t":
				if depth == 1 {
					capturingText = true
				}
			}
		case xml.CharData:
			if capturingText && depth == 1 {
				paragraph.Write([]byte(element))
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "t":
				capturingText = false
			case "p":
				if depth == 0 {
					continue
				}
				depth--
				if depth == 0 {
					if isDOCXHeadingStyle(style) {
						if heading := strings.TrimSpace(paragraph.String()); heading != "" {
							return heading, nil
						}
					}
					style = ""
					paragraph.Reset()
					capturingText = false
				}
			}
		}
	}
}

func attributeValue(element xml.StartElement, name string) string {
	for _, attribute := range element.Attr {
		if attribute.Name.Local == name {
			return attribute.Value
		}
	}

	return ""
}

func isDOCXHeadingStyle(style string) bool {
	normalized := strings.ToLower(strings.TrimSpace(style))
	if normalized == "title" {
		return true
	}
	if !strings.HasPrefix(normalized, "heading") {
		return false
	}

	level := strings.TrimPrefix(normalized, "heading")
	if len(level) != 1 {
		return false
	}

	return level[0] >= '1' && level[0] <= '6'
}

// markdownNodeText collects the visible text of an inline Markdown node.
func markdownNodeText(node ast.Node, source []byte) string {
	var builder strings.Builder

	_ = ast.Walk(node, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch value := child.(type) {
		case *ast.Text:
			builder.Write(value.Segment.Value(source))
			if value.SoftLineBreak() {
				builder.WriteByte(' ')
			}
		case *ast.String:
			builder.Write(value.Value)
		}

		return ast.WalkContinue, nil
	})

	return builder.String()
}
