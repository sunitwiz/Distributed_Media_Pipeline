package processor

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

func RunTranscode(workDir string, inputFile string) (string, error) {
	outputFile := filepath.Join(workDir, "output.mp4")

	cmd := exec.Command("ffmpeg",
		"-i", inputFile,
		"-vf", "scale=-2:480",
		"-c:a", "copy",
		"-y",
		outputFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg transcode failed: %s: %w", string(output), err)
	}

	return outputFile, nil
}
