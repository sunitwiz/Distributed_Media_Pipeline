package processor

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

func RunExtract(workDir string, inputFile string) (string, error) {
	outputFile := filepath.Join(workDir, "output.mp3")

	cmd := exec.Command("ffmpeg",
		"-i", inputFile,
		"-vn",
		"-acodec", "libmp3lame",
		"-y",
		outputFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg extract failed: %s: %w", string(output), err)
	}

	return outputFile, nil
}
