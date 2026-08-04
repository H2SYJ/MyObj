package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const DefaultProbeTimeout = 8 * time.Second

type Input struct {
	Path      string
	FileName  string
	MIME      string
	Size      int64
	Encrypted bool
}

type Value struct {
	Provider string
	Key      string
	Value    string
	Type     string
}

type Provider interface {
	Name() string
	Supports(Input) bool
	Extract(context.Context, Input) ([]Value, error)
}

type Result struct {
	Values  []Value
	Partial bool
	Errors  []error
}

func DefaultProviders() []Provider {
	return []Provider{BasicProvider{}, ImageProvider{}, FFProbeProvider{Timeout: DefaultProbeTimeout}}
}

func Extract(ctx context.Context, input Input, providers ...Provider) Result {
	if len(providers) == 0 {
		providers = DefaultProviders()
	}
	result := Result{Values: make([]Value, 0)}
	for _, provider := range providers {
		if !provider.Supports(input) {
			continue
		}
		values, err := provider.Extract(ctx, input)
		if err != nil {
			result.Partial = true
			result.Errors = append(result.Errors, fmt.Errorf("%s: %w", provider.Name(), err))
			continue
		}
		result.Values = append(result.Values, values...)
	}
	return result
}

func (r Result) ErrorText() string {
	if len(r.Errors) == 0 {
		return ""
	}
	parts := make([]string, 0, len(r.Errors))
	for _, err := range r.Errors {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "; ")
}

type BasicProvider struct{}

func (BasicProvider) Name() string        { return "basic" }
func (BasicProvider) Supports(Input) bool { return true }
func (BasicProvider) Extract(_ context.Context, input Input) ([]Value, error) {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(input.FileName)), ".")
	mainType := strings.SplitN(strings.ToLower(input.MIME), "/", 2)[0]
	sizeBucket := "small"
	if input.Size >= 1024*1024*1024 {
		sizeBucket = "large"
	} else if input.Size >= 100*1024*1024 {
		sizeBucket = "medium"
	}
	return []Value{
		{Provider: "basic", Key: "extension", Value: extension, Type: "string"},
		{Provider: "basic", Key: "mime", Value: input.MIME, Type: "string"},
		{Provider: "basic", Key: "mime_category", Value: mainType, Type: "string"},
		{Provider: "basic", Key: "size_bucket", Value: sizeBucket, Type: "string"},
		{Provider: "basic", Key: "encrypted", Value: strconv.FormatBool(input.Encrypted), Type: "boolean"},
	}, nil
}

type ImageProvider struct{}

func (ImageProvider) Name() string { return "image" }
func (ImageProvider) Supports(input Input) bool {
	return strings.HasPrefix(strings.ToLower(input.MIME), "image/")
}
func (ImageProvider) Extract(_ context.Context, input Input) ([]Value, error) {
	file, err := os.Open(input.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	config, format, err := image.DecodeConfig(file)
	if err != nil {
		return nil, fmt.Errorf("读取图片尺寸失败: %w", err)
	}
	return []Value{
		{Provider: "image", Key: "width", Value: strconv.Itoa(config.Width), Type: "integer"},
		{Provider: "image", Key: "height", Value: strconv.Itoa(config.Height), Type: "integer"},
		{Provider: "image", Key: "resolution", Value: resolutionName(config.Width, config.Height), Type: "string"},
		{Provider: "image", Key: "format", Value: strings.ToUpper(format), Type: "string"},
	}, nil
}

type FFProbeProvider struct {
	Path    string
	Timeout time.Duration
}

func (FFProbeProvider) Name() string { return "ffprobe" }
func (FFProbeProvider) Supports(input Input) bool {
	mime := strings.ToLower(input.MIME)
	return strings.HasPrefix(mime, "video/") || strings.HasPrefix(mime, "audio/")
}
func (provider FFProbeProvider) Extract(ctx context.Context, input Input) ([]Value, error) {
	path := provider.Path
	if path == "" {
		var err error
		path, err = exec.LookPath("ffprobe")
		if err != nil {
			return nil, errors.New("未找到 ffprobe")
		}
	}
	timeout := provider.Timeout
	if timeout <= 0 {
		timeout = DefaultProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(probeCtx, path,
		"-v", "error", "-show_entries",
		"stream=codec_type,codec_name,width,height:stream_tags=language:format=duration,format_name",
		"-of", "json", input.Path,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("ffprobe 探测超时")
		}
		return nil, fmt.Errorf("ffprobe 探测失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var output struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Tags      struct {
				Language string `json:"language"`
			} `json:"tags"`
		} `json:"streams"`
		Format struct {
			Duration   string `json:"duration"`
			FormatName string `json:"format_name"`
		} `json:"format"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return nil, fmt.Errorf("解析 ffprobe 输出失败: %w", err)
	}
	values := make([]Value, 0, 8)
	if output.Format.Duration != "" {
		values = append(values, Value{Provider: "ffprobe", Key: "duration", Value: output.Format.Duration, Type: "number"})
	}
	if output.Format.FormatName != "" {
		container := strings.Split(output.Format.FormatName, ",")[0]
		values = append(values, Value{Provider: "ffprobe", Key: "container", Value: strings.ToUpper(container), Type: "string"})
	}
	for _, stream := range output.Streams {
		switch stream.CodecType {
		case "video":
			if stream.CodecName != "" {
				values = append(values, Value{Provider: "ffprobe", Key: "video_codec", Value: stream.CodecName, Type: "string"})
			}
			if stream.Width > 0 && stream.Height > 0 {
				values = append(values,
					Value{Provider: "ffprobe", Key: "width", Value: strconv.Itoa(stream.Width), Type: "integer"},
					Value{Provider: "ffprobe", Key: "height", Value: strconv.Itoa(stream.Height), Type: "integer"},
					Value{Provider: "ffprobe", Key: "resolution", Value: resolutionName(stream.Width, stream.Height), Type: "string"},
				)
			}
		case "audio":
			if stream.CodecName != "" {
				values = append(values, Value{Provider: "ffprobe", Key: "audio_codec", Value: stream.CodecName, Type: "string"})
			}
		}
		if stream.Tags.Language != "" {
			values = append(values, Value{Provider: "ffprobe", Key: "language", Value: stream.Tags.Language, Type: "string"})
		}
	}
	return values, nil
}

func resolutionName(width, height int) string {
	shortSide := min(width, height)
	switch {
	case shortSide >= 4320:
		return "8K"
	case shortSide >= 2160:
		return "4K"
	case shortSide >= 1080:
		return "1080P"
	case shortSide >= 720:
		return "720P"
	default:
		return fmt.Sprintf("%dx%d", width, height)
	}
}
