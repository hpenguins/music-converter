package ffmpeg

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var cachedPath string

// locateFFmpeg 查找 ffmpeg 可执行文件位置
// 优先 PATH，后备：程序所在目录、当前工作目录
func locateFFmpeg() (string, error) {
	if cachedPath != "" {
		if _, err := os.Stat(cachedPath); err == nil {
			return cachedPath, nil
		}
		cachedPath = ""
	}

	// 1. PATH 中查找
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		cachedPath = p
		return p, nil
	}

	// 2. 程序所在目录
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates := []string{
			filepath.Join(dir, "ffmpeg.exe"),
			filepath.Join(dir, "ffmpeg"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				cachedPath = p
				return p, nil
			}
		}
	}

	// 3. 当前工作目录
	if wd, err := os.Getwd(); err == nil {
		candidates := []string{
			filepath.Join(wd, "ffmpeg.exe"),
			filepath.Join(wd, "ffmpeg"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				cachedPath = p
				return p, nil
			}
		}
	}

	return "", fmt.Errorf("ffmpeg 未找到。请将其加入 PATH，或放在程序同目录下")
}

// parseDuration 从 ffmpeg stderr 中提取 Duration 字段（秒）
func parseDuration(line string) float64 {
	re := regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+\.\d+)`)
	m := re.FindStringSubmatch(line)
	if m == nil {
		return 0
	}
	h, _ := strconv.ParseFloat(m[1], 64)
	mn, _ := strconv.ParseFloat(m[2], 64)
	s, _ := strconv.ParseFloat(m[3], 64)
	return h*3600 + mn*60 + s
}

// parseTime 从 ffmpeg 行中提取 time=HH:MM:SS.mmm 并转为秒
func parseTime(line string) float64 {
	re := regexp.MustCompile(`time=\s*(\d+):(\d+):(\d+\.\d+)`)
	m := re.FindStringSubmatch(line)
	if m == nil {
		return 0
	}
	h, _ := strconv.ParseFloat(m[1], 64)
	mn, _ := strconv.ParseFloat(m[2], 64)
	s, _ := strconv.ParseFloat(m[3], 64)
	return h*3600 + mn*60 + s
}

// Transcode 使用 ffmpeg 将 input 转码为指定格式输出
// format: "mp3", "flac", "ogg", "wav"
// onProgress: 进度回调 0~100，可能为 -1 表示未知
func Transcode(input, output, format string, onProgress func(pct float64)) error {
	ffpath, err := locateFFmpeg()
	if err != nil {
		return err
	}

	// 编码参数
	codecArgs := []string{}
	switch strings.ToLower(format) {
	case "mp3":
		codecArgs = []string{"-b:a", "320k", "-f", "mp3"}
	case "flac":
		codecArgs = []string{"-c:a", "flac"}
	case "ogg":
		codecArgs = []string{"-c:a", "libvorbis", "-q:a", "6"}
	case "wav":
		codecArgs = []string{"-c:a", "pcm_s16le"}
	default:
		return fmt.Errorf("不支持的输出格式: %s", format)
	}

	args := []string{
		"-y",
		"-i", input,
		"-vn",
	}
	args = append(args, codecArgs...)
	args = append(args, output)

	cmd := exec.Command(ffpath, args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("创建 stderr pipe 失败: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 ffmpeg 失败: %w", err)
	}

	// 解析 stderr 获取进度
	duration := 0.0
	lastPct := -1.0
	lastReport := time.Now()

	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if duration == 0 {
			if d := parseDuration(line); d > 0 {
				duration = d
			}
		}

		if strings.Contains(line, "time=") {
			current := parseTime(line)
			if duration > 0 && current > 0 {
				pct := math.Min(current/duration*100, 99.9)
				if time.Since(lastReport) > 200*time.Millisecond {
					if pct-lastPct >= 1.0 || lastPct < 0 {
						onProgress(pct)
						lastPct = pct
						lastReport = time.Now()
					}
				}
			} else if lastPct < 0 && time.Since(lastReport) > 500*time.Millisecond {
				onProgress(-1)
				lastReport = time.Now()
			}
		}
	}

	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("ffmpeg 转码失败: %w", err)
	}

	onProgress(100)
	return nil
}

// IsAvailable 检查 ffmpeg 是否可用
func IsAvailable() bool {
	_, err := locateFFmpeg()
	return err == nil
}
