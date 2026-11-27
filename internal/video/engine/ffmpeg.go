package engine

import (
	"fileforge-desktop/internal/platform/ffmpegex"
)

// The ffmpeg process runner, global flags and atomic publication live in
// internal/platform/ffmpegex so the audio domain reuses them. These aliases keep
// the video engine contract and every video test unchanged.

const (
	ffmpegStderrTailLines = ffmpegex.StderrTailLines
	ffmpegStderrTailBytes = ffmpegex.StderrTailBytes
)

var ffmpegGlobalArgs = ffmpegex.GlobalArgs

func withFFmpegGlobalArgs(args []string) []string {
	return ffmpegex.WithGlobalArgs(args)
}

// CommandRunner runs one external command.
type CommandRunner = ffmpegex.CommandRunner

// CaptureCommandRunner runs one ffmpeg command and returns its stdout and the
// bounded stderr tail. The audio sync normalization pass needs loudnorm's JSON
// summary, which only this runner shape exposes.
type CaptureCommandRunner = ffmpegex.CaptureCommandRunner

// ExecCommandRunner runs real ffmpeg processes through execx.
type ExecCommandRunner = ffmpegex.ExecCommandRunner

func tempOutputPath(outputPath string) (string, error) {
	return ffmpegex.TempOutputPath(outputPath)
}

func publishOutput(tempPath, outputPath string) error {
	return ffmpegex.PublishOutput(tempPath, outputPath)
}

func removeTempOutput(tempPath string) {
	ffmpegex.RemoveTempOutput(tempPath)
}
