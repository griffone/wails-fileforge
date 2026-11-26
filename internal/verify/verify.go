package verify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/models"
)

const (
	checkPath        = "path"
	checkExists      = "exists"
	checkRegularFile = "regular_file"
	checkReadable    = "readable"
	checkSize        = "size"
	checkKind        = "kind"
	checkImageDims   = "image_dimensions"
	checkPageCount   = "page_count"
	checkVideoLength = "video_duration"
	checkVideoDims   = "video_dimensions"
	checkVideoCodec  = "video_codec"
	checkAudioCodec  = "audio_codec"
	checkAudioLength = "audio_duration"
	checkTargetSize  = "target_size"

	kindImage = "image"
	kindPDF   = "pdf"
	kindVideo = "video"
	kindAudio = "audio"
)

// VerifyPaths verifies every path without a size target.
func VerifyPaths(paths []string) models.VerificationReport {
	return VerifyPathsWithTarget(paths, 0)
}

// VerifyPathsWithTarget verifies every output and, when targetBytes is
// positive, adds a target_size check per output. One bad output never hides
// the others: every path produces its own checks and warnings.
func VerifyPathsWithTarget(paths []string, targetBytes int64) models.VerificationReport {
	report := models.VerificationReport{
		Success: true,
		Outputs: make([]models.VerifiedOutput, 0, len(paths)),
	}
	if len(paths) == 0 {
		report.Success = false
		report.Message = "no outputs to verify"
		return report
	}

	totalChecks := 0
	failedChecks := 0
	advisoryFailures := 0
	for _, path := range paths {
		output := verifyOutput(path, targetBytes)
		report.Outputs = append(report.Outputs, output)
		report.Warnings = append(report.Warnings, output.Warnings...)

		for _, check := range output.Checks {
			totalChecks++
			if check.Passed {
				continue
			}
			if check.Advisory {
				advisoryFailures++
				continue
			}
			failedChecks++
		}
	}

	if failedChecks > 0 {
		report.Success = false
		report.Message = fmt.Sprintf("verification failed: %d of %d checks failed", failedChecks, totalChecks)
		return report
	}

	report.Message = fmt.Sprintf("verified %d %s", len(report.Outputs), pluralize("output", len(report.Outputs)))
	if advisoryFailures > 0 {
		report.Message = fmt.Sprintf("%s with %d advisory %s", report.Message, advisoryFailures, pluralize("warning", advisoryFailures))
	}
	return report
}

func verifyOutput(path string, targetBytes int64) models.VerifiedOutput {
	trimmed := strings.TrimSpace(path)
	info := inspect.InspectFile(trimmed)
	output := models.VerifiedOutput{
		Path:      trimmed,
		Exists:    info.Exists,
		SizeBytes: info.SizeBytes,
		Kind:      info.Kind,
		Checks:    make([]models.VerificationCheck, 0, 8),
	}

	output.Checks = append(output.Checks, models.VerificationCheck{
		Key:    checkPath,
		Label:  "Path",
		Passed: trimmed != "",
		Value:  trimmed,
		Detail: checkDetail(trimmed == "", "path is empty"),
	})

	if !info.Exists {
		output.Checks = append(output.Checks, failedCheck(checkExists, "Exists", info.Error))
		output.Warnings = append(output.Warnings, fmt.Sprintf("%s: %s", displayPath(trimmed), fallbackMessage(info.Error, "file cannot be accessed")))
		return output
	}
	output.Checks = append(output.Checks, passedCheck(checkExists, "Exists", "true"))

	regular, regularDetail := regularFile(trimmed)
	output.Checks = append(output.Checks, boolCheck(checkRegularFile, "Regular file", regular, regularDetail))
	if !regular {
		output.Warnings = append(output.Warnings, fmt.Sprintf("%s: %s", displayPath(trimmed), regularDetail))
		return output
	}

	readable, readableDetail := readable(trimmed)
	output.Checks = append(output.Checks, boolCheck(checkReadable, "Readable", readable, readableDetail))
	if !readable {
		output.Warnings = append(output.Warnings, fmt.Sprintf("%s: %s", displayPath(trimmed), readableDetail))
	}

	nonEmpty := info.SizeBytes > 0
	output.Checks = append(output.Checks, models.VerificationCheck{
		Key:    checkSize,
		Label:  "Size",
		Passed: nonEmpty,
		Value:  formatByteSize(info.SizeBytes),
		Detail: checkDetail(!nonEmpty, "file is empty"),
	})
	if !nonEmpty {
		output.Warnings = append(output.Warnings, fmt.Sprintf("%s: file is empty", displayPath(trimmed)))
	}

	kindKnown := info.Extension != "" && info.Kind != "other"
	output.Checks = append(output.Checks, models.VerificationCheck{
		Key:    checkKind,
		Label:  "File type",
		Passed: kindKnown,
		Value:  info.Kind,
		Detail: checkDetail(!kindKnown, "file type could not be classified"),
	})
	if !kindKnown {
		output.Warnings = append(output.Warnings, fmt.Sprintf("%s: file type could not be classified", displayPath(trimmed)))
	}

	output.Checks = append(output.Checks, mediaChecks(info, &output)...)

	if targetBytes > 0 {
		targetMet := info.SizeBytes <= targetBytes
		output.ActualBytes = info.SizeBytes
		output.TargetBytes = targetBytes
		output.Checks = append(output.Checks, models.VerificationCheck{
			Key:      checkTargetSize,
			Label:    "Target size",
			Passed:   targetMet,
			Advisory: true,
			Value:    fmt.Sprintf("%s / %s", formatByteSize(info.SizeBytes), formatByteSize(targetBytes)),
			Detail: checkDetail(!targetMet, fmt.Sprintf(
				"output is %s; target is %s",
				formatByteSize(info.SizeBytes),
				formatByteSize(targetBytes),
			)),
		})
		if !targetMet {
			output.Warnings = append(output.Warnings, fmt.Sprintf(
				"%s: %s exceeds the %s target",
				displayPath(trimmed),
				formatByteSize(info.SizeBytes),
				formatByteSize(targetBytes),
			))
		}
	}

	return output
}

func mediaChecks(info models.FileInfo, output *models.VerifiedOutput) []models.VerificationCheck {
	switch info.Kind {
	case kindImage:
		return imageChecks(info, output)
	case kindPDF:
		pages := info.PageCount
		return []models.VerificationCheck{{
			Key:    checkPageCount,
			Label:  "Page count",
			Passed: pages > 0,
			Value:  fmt.Sprintf("%d %s", pages, pluralize("page", pages)),
			Detail: checkDetail(pages <= 0, "PDF pages could not be read"),
		}}
	case kindVideo:
		return videoChecks(info, output)
	case kindAudio:
		return audioChecks(info, output)
	default:
		return nil
	}
}

func imageChecks(info models.FileInfo, output *models.VerifiedOutput) []models.VerificationCheck {
	if !supportsDimensionInspection(info.Extension) {
		output.Warnings = append(output.Warnings, fmt.Sprintf(
			"%s: dimensions are not inspected for .%s images",
			displayPath(info.Path),
			info.Extension,
		))
		return nil
	}

	hasDimensions := info.Width > 0 && info.Height > 0
	return []models.VerificationCheck{{
		Key:    checkImageDims,
		Label:  "Dimensions",
		Passed: hasDimensions,
		Value:  fmt.Sprintf("%d × %d px", info.Width, info.Height),
		Detail: checkDetail(!hasDimensions, "image dimensions could not be decoded"),
	}}
}

func videoChecks(info models.FileInfo, output *models.VerifiedOutput) []models.VerificationCheck {
	checks := make([]models.VerificationCheck, 0, 4)

	if info.DurationSeconds > 0 {
		checks = append(checks, models.VerificationCheck{
			Key:    checkVideoLength,
			Label:  "Duration",
			Passed: true,
			Value:  fmt.Sprintf("%.1f s", info.DurationSeconds),
		})
	}

	if info.Width > 0 && info.Height > 0 {
		checks = append(checks, models.VerificationCheck{
			Key:    checkVideoDims,
			Label:  "Dimensions",
			Passed: true,
			Value:  fmt.Sprintf("%d × %d px", info.Width, info.Height),
		})
	}

	if info.VideoCodec != "" {
		checks = append(checks, models.VerificationCheck{
			Key:    checkVideoCodec,
			Label:  "Video codec",
			Passed: true,
			Value:  info.VideoCodec,
		})
	}

	if info.AudioCodec != "" {
		checks = append(checks, models.VerificationCheck{
			Key:    checkAudioCodec,
			Label:  "Audio codec",
			Passed: true,
			Value:  info.AudioCodec,
		})
	}

	if len(checks) > 0 {
		return checks
	}

	output.Warnings = append(output.Warnings, fmt.Sprintf(
		"%s: video duration could not be determined (ffprobe unavailable or the file is unreadable)",
		displayPath(info.Path),
	))
	return nil
}

func audioChecks(info models.FileInfo, output *models.VerifiedOutput) []models.VerificationCheck {
	metadata, ok := inspect.ProbeVideoMetadata(context.Background(), info.Path)
	duration := 0.0
	codec := ""
	if ok {
		duration = metadata.DurationSeconds
		codec = strings.ToLower(strings.TrimSpace(metadata.AudioCodec))
	} else {
		output.Warnings = append(output.Warnings, fmt.Sprintf(
			"%s: audio metadata could not be read (ffprobe is unavailable or the file is truncated)",
			displayPath(info.Path),
		))
	}

	durationPassed := duration > 0
	codecPassed := codec != ""

	return []models.VerificationCheck{
		{
			Key:    checkAudioLength,
			Label:  "Duration",
			Passed: durationPassed,
			Value:  fmt.Sprintf("%.1f s", duration),
			Detail: checkDetail(!durationPassed, "audio duration is zero or missing"),
		},
		{
			Key:    checkAudioCodec,
			Label:  "Audio codec",
			Passed: codecPassed,
			Value:  codec,
			Detail: checkDetail(!codecPassed, "no audio stream was found"),
		},
	}
}

func regularFile(path string) (bool, string) {
	info, err := os.Stat(path)
	if err != nil {
		return false, "file cannot be accessed"
	}
	if !info.Mode().IsRegular() {
		return false, "path is not a regular file"
	}

	return true, ""
}

func readable(path string) (bool, string) {
	file, err := os.Open(path)
	if err != nil {
		return false, "file is not readable"
	}
	defer func() {
		_ = file.Close()
	}()

	buffer := make([]byte, 1)
	if _, readErr := file.Read(buffer); readErr != nil && !errors.Is(readErr, io.EOF) {
		return false, "file is not readable"
	}

	return true, ""
}

func supportsDimensionInspection(extension string) bool {
	switch extension {
	case "jpg", "jpeg", "png", "gif", "webp":
		return true
	default:
		return false
	}
}

func boolCheck(key, label string, passed bool, detail string) models.VerificationCheck {
	check := models.VerificationCheck{
		Key:    key,
		Label:  label,
		Passed: passed,
	}
	if passed {
		check.Value = "true"
	} else {
		check.Detail = detail
	}

	return check
}

func passedCheck(key, label, value string) models.VerificationCheck {
	return models.VerificationCheck{Key: key, Label: label, Passed: true, Value: value}
}

func failedCheck(key, label, detail string) models.VerificationCheck {
	return models.VerificationCheck{Key: key, Label: label, Passed: false, Detail: detail}
}

func checkDetail(condition bool, detail string) string {
	if condition {
		return detail
	}

	return ""
}

func fallbackMessage(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}

	return value
}

func displayPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "output"
	}

	return trimmed
}

func pluralize(word string, count int) string {
	if count == 1 {
		return word
	}

	return word + "s"
}

func formatByteSize(bytes int64) string {
	if bytes < 0 {
		bytes = 0
	}

	const (
		kib = 1024.0
		mib = kib * 1024.0
		gib = mib * 1024.0
	)

	switch {
	case float64(bytes) >= gib:
		return fmt.Sprintf("%.1f GB", float64(bytes)/gib)
	case float64(bytes) >= mib:
		return fmt.Sprintf("%.1f MB", float64(bytes)/mib)
	case float64(bytes) >= kib:
		return fmt.Sprintf("%.0f KB", float64(bytes)/kib)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
