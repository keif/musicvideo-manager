package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ProbeResult holds the ffprobe-derived metadata for a video file. A
// zero-value field (0 or "") means ffprobe did not report that value —
// none of these fields can legitimately be exactly zero for a real video.
type ProbeResult struct {
	DurationSeconds float64
	Width           int
	Height          int
	VideoCodec      string
	AudioCodec      string
	Container       string
	Bitrate         int64
	FPS             float64
}

type ffprobeFormat struct {
	Duration   string `json:"duration"`
	BitRate    string `json:"bit_rate"`
	FormatName string `json:"format_name"`
}

type ffprobeStream struct {
	CodecType  string `json:"codec_type"`
	CodecName  string `json:"codec_name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RFrameRate string `json:"r_frame_rate"`
}

type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

// Probe runs ffprobe against path and parses out the fields the catalog cares about.
func Probe(ctx context.Context, path string) (ProbeResult, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_format", "-show_streams",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return ProbeResult{}, fmt.Errorf("ffprobe %s: %w", path, err)
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return ProbeResult{}, fmt.Errorf("parse ffprobe output for %s: %w", path, err)
	}

	result := ProbeResult{Container: parsed.Format.FormatName}
	if d, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
		result.DurationSeconds = d
	}
	if b, err := strconv.ParseInt(parsed.Format.BitRate, 10, 64); err == nil {
		result.Bitrate = b
	}

	for _, s := range parsed.Streams {
		switch s.CodecType {
		case "video":
			if result.VideoCodec == "" {
				result.VideoCodec = s.CodecName
				result.Width = s.Width
				result.Height = s.Height
				result.FPS = parseFrameRate(s.RFrameRate)
			}
		case "audio":
			if result.AudioCodec == "" {
				result.AudioCodec = s.CodecName
			}
		}
	}
	return result, nil
}

func parseFrameRate(s string) float64 {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return 0
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0
	}
	return num / den
}
