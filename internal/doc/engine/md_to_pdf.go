package engine

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	ghtml "github.com/yuin/goldmark/renderer/html"
)

const (
	defaultBodyFontSize  = 11
	baseLeftMarginMM     = 15.0
	baseRightMarginMM    = 15.0
	baseTopMarginMM      = 20.0
	baseBottomMarginMM   = 18.0
	headerFooterLineMM   = 6.0
	headerFooterFontSize = 9.0

	remoteImageTimeout  = 8 * time.Second
	maxRemoteImageBytes = 20 << 20 // 20 MiB
)

var remoteImageHTTPClient = &http.Client{Timeout: remoteImageTimeout}

type HeaderFooterConfig struct {
	Enabled      bool
	Text         string
	Align        string
	Font         string
	MarginTop    float64
	MarginBottom float64
	Color        string
}

type RenderConfig struct {
	InputPath  string
	OutputPath string
	Header     HeaderFooterConfig
	Footer     HeaderFooterConfig
}

type renderPlan struct {
	TopMargin    float64
	BottomMargin float64
	Header       headerFooterPlan
	Footer       headerFooterPlan
}

type headerFooterPlan struct {
	Enabled      bool
	Y            float64
	Align        string
	Font         string
	R            int
	G            int
	B            int
	TextTemplate string
}

var (
	h1Regexp        = regexp.MustCompile(`^\s*<h1[^>]*>(.*?)</h1>\s*$`)
	h2Regexp        = regexp.MustCompile(`^\s*<h2[^>]*>(.*?)</h2>\s*$`)
	h3Regexp        = regexp.MustCompile(`^\s*<h3[^>]*>(.*?)</h3>\s*$`)
	listItemRegexp  = regexp.MustCompile(`^\s*<li[^>]*>(.*?)</li>\s*$`)
	imgTagRegexp    = regexp.MustCompile(`<img[^>]*src=["']([^"']+)["'][^>]*>`)
	pTagRegexp      = regexp.MustCompile(`^\s*<p[^>]*>(.*?)</p>\s*$`)
	lineBreakRegexp = regexp.MustCompile(`(?i)<br\s*/?>`)
	tagStripRegexp  = regexp.MustCompile(`<[^>]+>`)
)

func RenderMarkdownToPDF(ctx context.Context, cfg RenderConfig) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("render cancelled: %w", ctx.Err())
	default:
	}

	raw, err := os.ReadFile(cfg.InputPath)
	if err != nil {
		return fmt.Errorf("read markdown: %w", err)
	}

	htmlRendered, err := markdownToHTML(raw)
	if err != nil {
		return fmt.Errorf("markdown render: %w", err)
	}

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetCreator("FileForge", true)
	pdf.SetTitle(filepath.Base(cfg.InputPath), true)

	plan := buildRenderPlan(cfg)
	pdf.SetMargins(baseLeftMarginMM, plan.TopMargin, baseRightMarginMM)
	pdf.SetAutoPageBreak(true, plan.BottomMargin)

	installHeaderFooter(pdf, cfg, plan)

	pdf.AddPage()
	pdf.SetFont("Helvetica", "", defaultBodyFontSize)
	if err := drawHTMLApprox(ctx, pdf, htmlRendered, filepath.Dir(cfg.InputPath)); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("render cancelled: %w", ctx.Err())
	default:
	}

	if err := pdf.OutputFileAndClose(cfg.OutputPath); err != nil {
		return fmt.Errorf("write pdf: %w", err)
	}

	return nil
}

func markdownToHTML(markdown []byte) (string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
		),
		goldmark.WithRendererOptions(
			ghtml.WithUnsafe(),
		),
	)

	var builder strings.Builder
	if err := md.Convert(markdown, &builder); err != nil {
		return "", err
	}

	return builder.String(), nil
}

func installHeaderFooter(pdf *gofpdf.Fpdf, cfg RenderConfig, plan renderPlan) {
	fileName := filepath.Base(cfg.InputPath)
	dateText := time.Now().Format("2006-01-02")

	pdf.SetHeaderFuncMode(func() {
		if !plan.Header.Enabled {
			return
		}

		pdf.SetY(plan.Header.Y)
		pdf.SetFont(plan.Header.Font, "", headerFooterFontSize)
		pdf.SetTextColor(plan.Header.R, plan.Header.G, plan.Header.B)
		text := expandPlaceholders(plan.Header.TextTemplate, pdf.PageNo(), "{nb}", dateText, fileName)
		pdf.CellFormat(0, headerFooterLineMM, text, "", 0, plan.Header.Align, false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	}, true)

	pdf.AliasNbPages("{nb}")
	pdf.SetFooterFunc(func() {
		if !plan.Footer.Enabled {
			return
		}

		pdf.SetY(plan.Footer.Y)
		pdf.SetFont(plan.Footer.Font, "", headerFooterFontSize)
		pdf.SetTextColor(plan.Footer.R, plan.Footer.G, plan.Footer.B)
		text := expandPlaceholders(plan.Footer.TextTemplate, pdf.PageNo(), "{nb}", dateText, fileName)
		pdf.CellFormat(0, headerFooterLineMM, text, "", 0, plan.Footer.Align, false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	})
}

func buildRenderPlan(cfg RenderConfig) renderPlan {
	plan := renderPlan{
		TopMargin:    baseTopMarginMM,
		BottomMargin: baseBottomMarginMM,
		Header:       buildHeaderPlan(cfg.Header),
		Footer:       buildFooterPlan(cfg.Footer),
	}

	if plan.Header.Enabled {
		topCandidate := nonNegative(cfg.Header.MarginTop) + headerFooterLineMM + nonNegative(cfg.Header.MarginBottom)
		if topCandidate > plan.TopMargin {
			plan.TopMargin = topCandidate
		}
	}

	if plan.Footer.Enabled {
		bottomCandidate := nonNegative(cfg.Footer.MarginTop) + headerFooterLineMM + nonNegative(cfg.Footer.MarginBottom)
		if bottomCandidate > plan.BottomMargin {
			plan.BottomMargin = bottomCandidate
		}
	}

	return plan
}

func buildHeaderPlan(cfg HeaderFooterConfig) headerFooterPlan {
	r, g, b := colorRGB(cfg.Color)
	return headerFooterPlan{
		Enabled:      cfg.Enabled,
		Y:            nonNegative(cfg.MarginTop),
		Align:        toFPDFAlign(cfg.Align),
		Font:         normalizeFont(cfg.Font),
		R:            r,
		G:            g,
		B:            b,
		TextTemplate: cfg.Text,
	}
}

func buildFooterPlan(cfg HeaderFooterConfig) headerFooterPlan {
	r, g, b := colorRGB(cfg.Color)
	return headerFooterPlan{
		Enabled:      cfg.Enabled,
		Y:            -(nonNegative(cfg.MarginBottom) + headerFooterLineMM),
		Align:        toFPDFAlign(cfg.Align),
		Font:         normalizeFont(cfg.Font),
		R:            r,
		G:            g,
		B:            b,
		TextTemplate: cfg.Text,
	}
}

func nonNegative(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

func drawHTMLApprox(ctx context.Context, pdf *gofpdf.Fpdf, htmlSource, baseDir string) error {
	lines := strings.Split(htmlSource, "\n")
	for _, line := range lines {
		select {
		case <-ctx.Done():
			return fmt.Errorf("render cancelled: %w", ctx.Err())
		default:
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if src, ok := extractImageSource(trimmed); ok {
			if err := drawImage(ctx, pdf, src, baseDir); err != nil {
				return fmt.Errorf("image render failed (%s): %w", src, err)
			}
			continue
		}

		switch {
		case h1Regexp.MatchString(trimmed):
			writeHeading(pdf, h1Regexp.ReplaceAllString(trimmed, "$1"), 18)
		case h2Regexp.MatchString(trimmed):
			writeHeading(pdf, h2Regexp.ReplaceAllString(trimmed, "$1"), 15)
		case h3Regexp.MatchString(trimmed):
			writeHeading(pdf, h3Regexp.ReplaceAllString(trimmed, "$1"), 13)
		case listItemRegexp.MatchString(trimmed):
			content := normalizeText(listItemRegexp.ReplaceAllString(trimmed, "$1"))
			pdf.SetFont("Helvetica", "", defaultBodyFontSize)
			pdf.MultiCell(0, 5.5, "• "+content, "", "L", false)
		case pTagRegexp.MatchString(trimmed):
			content := normalizeText(pTagRegexp.ReplaceAllString(trimmed, "$1"))
			pdf.SetFont("Helvetica", "", defaultBodyFontSize)
			pdf.MultiCell(0, 5.5, content, "", "L", false)
		case strings.HasPrefix(trimmed, "<"):
			content := normalizeText(trimmed)
			if content != "" {
				pdf.SetFont("Helvetica", "", defaultBodyFontSize)
				pdf.MultiCell(0, 5.5, content, "", "L", false)
			}
		default:
			pdf.SetFont("Helvetica", "", defaultBodyFontSize)
			pdf.MultiCell(0, 5.5, html.UnescapeString(trimmed), "", "L", false)
		}
	}

	return nil
}

func writeHeading(pdf *gofpdf.Fpdf, text string, size float64) {
	pdf.SetFont("Helvetica", "B", size)
	pdf.MultiCell(0, 7, normalizeText(text), "", "L", false)
	pdf.Ln(1)
}

func drawImage(ctx context.Context, pdf *gofpdf.Fpdf, src, baseDir string) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("render cancelled: %w", ctx.Err())
	default:
	}

	if isRemoteImageSource(src) {
		return drawRemoteImage(ctx, pdf, src)
	}

	imagePath := src
	if !filepath.IsAbs(imagePath) {
		imagePath = filepath.Join(baseDir, src)
	}
	imagePath = filepath.Clean(imagePath)

	if _, err := os.Stat(imagePath); err != nil {
		return fmt.Errorf("image not found")
	}

	currentY := pdf.GetY()
	maxWidth := 180.0
	imageOpts := gofpdf.ImageOptions{ImageType: "", ReadDpi: true}
	pdf.ImageOptions(imagePath, 15, currentY+2, maxWidth, 0, false, imageOpts, 0, "")
	pdf.Ln(65)
	return nil
}

func isRemoteImageSource(src string) bool {
	source := strings.ToLower(strings.TrimSpace(src))
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}

func drawRemoteImage(ctx context.Context, pdf *gofpdf.Fpdf, src string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return fmt.Errorf("invalid remote image url: %w", err)
	}

	resp, err := remoteImageHTTPClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("remote image download cancelled: %w", cancellationCause(ctx, err))
		}
		return fmt.Errorf("remote image download failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("remote image download failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteImageBytes+1))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("remote image download cancelled: %w", cancellationCause(ctx, err))
		}
		return fmt.Errorf("read remote image body: %w", err)
	}

	if len(body) == 0 {
		return fmt.Errorf("remote image download returned empty body")
	}

	if len(body) > maxRemoteImageBytes {
		return fmt.Errorf("remote image exceeds size limit (%d bytes)", maxRemoteImageBytes)
	}

	imageType, ok := detectImageType(body)
	if !ok {
		return fmt.Errorf("remote image format is not supported")
	}

	resourceName := remoteImageResourceName(src, body)
	imageOpts := gofpdf.ImageOptions{ImageType: imageType, ReadDpi: true}
	pdf.RegisterImageOptionsReader(resourceName, imageOpts, bytes.NewReader(body))

	currentY := pdf.GetY()
	maxWidth := 180.0
	pdf.ImageOptions(resourceName, 15, currentY+2, maxWidth, 0, false, imageOpts, 0, "")
	pdf.Ln(65)

	return nil
}

func detectImageType(content []byte) (string, bool) {
	contentType := http.DetectContentType(content)
	switch contentType {
	case "image/png":
		return "png", true
	case "image/jpeg":
		return "jpg", true
	case "image/gif":
		return "gif", true
	default:
		return "", false
	}
}

func remoteImageResourceName(src string, content []byte) string {
	sum := sha1.Sum(append([]byte(src), content...))
	return fmt.Sprintf("remote-img-%x", sum)
}

func cancellationCause(ctx context.Context, fallback error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return fallback
}

func extractImageSource(line string) (string, bool) {
	match := imgTagRegexp.FindStringSubmatch(line)
	if len(match) >= 2 {
		return strings.TrimSpace(match[1]), true
	}

	if strings.HasPrefix(line, "![") {
		open := strings.Index(line, "(")
		close := strings.LastIndex(line, ")")
		if open >= 0 && close > open {
			return strings.TrimSpace(line[open+1 : close]), true
		}
	}

	return "", false
}

func normalizeText(content string) string {
	replacedBreaks := lineBreakRegexp.ReplaceAllString(content, "\n")
	noTags := tagStripRegexp.ReplaceAllString(replacedBreaks, "")
	decoded := html.UnescapeString(noTags)
	return strings.TrimSpace(decoded)
}

func expandPlaceholders(template string, page int, totalPages, dateText, fileName string) string {
	replacer := strings.NewReplacer(
		"{page}", strconv.Itoa(page),
		"{totalPages}", totalPages,
		"{date}", dateText,
		"{fileName}", fileName,
	)
	return replacer.Replace(template)
}

func colorRGB(hex string) (int, int, int) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(trimmed) != 6 {
		return 0, 0, 0
	}

	r, errR := strconv.ParseInt(trimmed[0:2], 16, 64)
	g, errG := strconv.ParseInt(trimmed[2:4], 16, 64)
	b, errB := strconv.ParseInt(trimmed[4:6], 16, 64)
	if errR != nil || errG != nil || errB != nil {
		return 0, 0, 0
	}

	return int(r), int(g), int(b)
}

func normalizeFont(font string) string {
	switch strings.ToLower(strings.TrimSpace(font)) {
	case "times":
		return "Times"
	case "courier":
		return "Courier"
	default:
		return "Helvetica"
	}
}

func toFPDFAlign(align string) string {
	switch strings.ToLower(strings.TrimSpace(align)) {
	case "center":
		return "C"
	case "right":
		return "R"
	default:
		return "L"
	}
}
