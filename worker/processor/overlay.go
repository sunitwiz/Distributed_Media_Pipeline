package processor

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

func RunOverlay(workDir string, inputFile string, subtitleFile string) (string, error) {
	outputFile := filepath.Join(workDir, "output.mp4")

	cmd := exec.Command("ffmpeg",
		"-i", inputFile,
		"-vf", fmt.Sprintf("subtitles=%s", subtitleFile),
		"-y",
		outputFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg overlay failed: %s: %w", string(output), err)
	}

	return outputFile, nil
}
