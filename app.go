package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"music-converter/ffmpeg"
)

// AppSettings 用户设置
type AppSettings struct {
	SaveCoverFile bool `json:"saveCoverFile"`
}

// App struct
type App struct {
	ctx      context.Context
	settings AppSettings
}

// settingsPath 返回设置文件的路径
func settingsPath() string {
	dir := filepath.Join(os.Getenv("APPDATA"), "ncm-converter")
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "settings.json")
}

// loadSettings 从磁盘加载设置
func loadSettings() AppSettings {
	s := AppSettings{SaveCoverFile: true}
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return s
	}
	json.Unmarshal(data, &s)
	return s
}

// saveSettings 保存设置到磁盘
func saveSettings(s AppSettings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化设置失败: %w", err)
	}
	return os.WriteFile(settingsPath(), data, 0644)
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		settings: loadSettings(),
	}
}

// startup 应用启动时调用
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	runtime.OnFileDrop(ctx, func(x, y int, paths []string) {
		if len(paths) == 0 {
			return
		}
		runtime.EventsEmit(ctx, "wails:dragdrop", paths)
	})
}

// GetSettings 返回当前设置
func (a *App) GetSettings() AppSettings {
	return a.settings
}

// SaveSettings 保存设置
func (a *App) SaveSettings(s AppSettings) error {
	a.settings = s
	return saveSettings(s)
}

// FileInfo 文件信息
type FileInfo struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Selected bool   `json:"selected"`
}

// DecryptStatus 解密状态
type DecryptStatus struct {
	FileName  string `json:"fileName"`
	FilePath  string `json:"filePath"`
	Status    string `json:"status"`
	Progress  int    `json:"progress"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	Title     string `json:"title,omitempty"`
	Artist    string `json:"artist,omitempty"`
	Album     string `json:"album,omitempty"`
	Format    string `json:"format,omitempty"`
	CoverPath string `json:"coverPath,omitempty"`
}

// FileConvertRequest 文件转换请求
type FileConvertRequest struct {
	Path   string `json:"path"`
	Format string `json:"format"` // "auto", "mp3", "flac", "ogg", "wav"
}

// SelectNCMFiles 打开文件选择对话框
func (a *App) SelectNCMFiles() ([]FileInfo, error) {
	files, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择音频文件",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "所有支持的格式",
				Pattern:     "*.ncm;*.mp3;*.flac;*.ogg;*.wav;*.m4a;*.wma;*.aac;*.opus",
			},
			{
				DisplayName: "NCM 文件 (*.ncm)",
				Pattern:     "*.ncm",
			},
			{
				DisplayName: "音频文件 (*.mp3;*.flac;*.ogg;*.wav;*.m4a)",
				Pattern:     "*.mp3;*.flac;*.ogg;*.wav;*.m4a",
			},
			{
				DisplayName: "所有文件 (*.*)",
				Pattern:     "*",
			},
		},
	})
	if err != nil {
		return nil, err
	}

	var result []FileInfo
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		result = append(result, FileInfo{
			Path: f,
			Name: filepath.Base(f),
			Size: info.Size(),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// SelectOutputDir 打开文件夹选择对话框
func (a *App) SelectOutputDir() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择输出文件夹",
	})
	if err != nil {
		return "", err
	}
	return dir, nil
}

// GetDefaultOutputDir 获取默认下载文件夹
func (a *App) GetDefaultOutputDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	downloads := filepath.Join(home, "Downloads")
	if info, err := os.Stat(downloads); err == nil && info.IsDir() {
		return downloads
	}
	return home
}

// CheckFFmpeg 返回 ffmpeg 是否可用
func (a *App) CheckFFmpeg() bool {
	return ffmpeg.IsAvailable()
}

// GetCoverAsBase64 读取封面图片并返回 Base64 数据 URI
func (a *App) GetCoverAsBase64(coverPath string) string {
	if coverPath == "" {
		return ""
	}
	data, err := os.ReadFile(coverPath)
	if err != nil {
		return ""
	}
	mime := "image/jpeg"
	ext := filepath.Ext(coverPath)
	switch ext {
	case ".png":
		mime = "image/png"
	case ".webp":
		mime = "image/webp"
	case ".gif":
		mime = "image/gif"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// isNCM 判断是否为 NCM 文件
func isNCM(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == ".ncm"
}

// isSupportedAudio 判断是否为支持的音频文件
func isSupportedAudio(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	supported := []string{".ncm", ".mp3", ".flac", ".ogg", ".wav", ".m4a", ".wma", ".aac", ".opus"}
	for _, e := range supported {
		if ext == e {
			return true
		}
	}
	return false
}

// ConvertFiles 批量转换（NCM 解密 + 通用音频转码）
func (a *App) ConvertFiles(requests []FileConvertRequest, outputDir string) ([]DecryptStatus, error) {
	if len(requests) == 0 {
		return nil, fmt.Errorf("没有选择文件")
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("创建输出目录失败: %w", err)
	}

	results := make([]DecryptStatus, len(requests))
	total := len(requests)

	for i, req := range requests {
		filePath := req.Path
		fileName := filepath.Base(filePath)
		targetFormat := strings.ToLower(req.Format)

		if targetFormat == "" || targetFormat == "auto" {
			if isNCM(filePath) {
				targetFormat = "detect"
			} else {
				ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
				if ext == "m4a" || ext == "wma" || ext == "aac" || ext == "opus" {
					targetFormat = "mp3"
				} else {
					targetFormat = ext
				}
			}
		}

		results[i] = DecryptStatus{FileName: fileName, FilePath: filePath, Status: "processing"}

		if isNCM(filePath) {
			err := a.convertNCM(i, total, req, targetFormat, outputDir, &results[i])
			if err != nil {
				results[i].Status = "error"
				results[i].Error = err.Error()
			}
		} else {
			err := a.convertAudio(i, total, req, targetFormat, outputDir, &results[i])
			if err != nil {
				results[i].Status = "error"
				results[i].Error = err.Error()
			}
		}

		runtime.EventsEmit(a.ctx, "convert:progress", map[string]interface{}{
			"current":   i + 1,
			"total":     total,
			"fileName":  fileName,
			"status":    results[i].Status,
			"error":     results[i].Error,
			"output":    results[i].Output,
			"title":     results[i].Title,
			"artist":    results[i].Artist,
				"album":     results[i].Album,
			"format":    results[i].Format,
			"coverPath": results[i].CoverPath,
		})
	}

	return results, nil
}

// convertNCM 处理 NCM 文件：解密 + 可选 ffmpeg 转码
func (a *App) convertNCM(idx, total int, req FileConvertRequest, targetFormat, outputDir string, status *DecryptStatus) error {
	filePath := req.Path
	fileName := filepath.Base(filePath)

	runtime.EventsEmit(a.ctx, "convert:progress", map[string]interface{}{
		"current": idx, "total": total, "fileName": fileName, "status": "decrypting",
	})

	result, audioData, err := DecryptToBuffer(filePath)
	if err != nil {
		return fmt.Errorf("解密失败: %w", err)
	}

	if targetFormat == "detect" {
		targetFormat = result.Format
	}

	needTranscode := targetFormat != result.Format
	baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	outName := baseName + "." + targetFormat
	outPath := filepath.Join(outputDir, outName)

	if needTranscode {
		tmpDir := filepath.Join(os.TempDir(), "ncm-converter")
		os.MkdirAll(tmpDir, 0755)
		tmpFile := filepath.Join(tmpDir, baseName+"."+result.Format)
		if err := os.WriteFile(tmpFile, audioData, 0644); err != nil {
			return fmt.Errorf("写临时文件失败: %w", err)
		}
		if err := EmbedMetadata(tmpFile, result.Title, result.Artist, result.Album, result.CoverData); err != nil {
			fmt.Printf("嵌入临时文件元数据失败: %v\n", err)
		}

		runtime.EventsEmit(a.ctx, "convert:progress", map[string]interface{}{
			"current": idx, "total": total, "fileName": fileName, "status": "transcoding",
		})
		tErr := ffmpeg.Transcode(tmpFile, outPath, targetFormat, func(pct float64) {
			runtime.EventsEmit(a.ctx, "convert:progress", map[string]interface{}{
				"current": idx, "total": total, "fileName": fileName, "status": "transcoding", "transPct": pct,
			})
		})
		os.Remove(tmpFile)
		if tErr != nil {
			return fmt.Errorf("转码失败: %w", tErr)
		}

		if err := EmbedMetadata(outPath, result.Title, result.Artist, result.Album, result.CoverData); err != nil {
			fmt.Printf("嵌入元数据失败: %v\n", err)
		}
	} else {
		if err := WriteAudioFile(outPath, audioData); err != nil {
			return fmt.Errorf("写入文件失败: %w", err)
		}
		if err := EmbedMetadata(outPath, result.Title, result.Artist, result.Album, result.CoverData); err != nil {
			fmt.Printf("嵌入元数据失败: %v\n", err)
		}
	}

	coverPath := ""
	if a.settings.SaveCoverFile && result.CoverData != nil {
		coverPath = WriteCoverFile(result.CoverData, outPath)
	}

	status.Status = "success"
	status.Output = outPath
	status.Title = result.Title
	status.Artist = result.Artist
	status.Album = result.Album
	status.Format = targetFormat
	status.CoverPath = coverPath
	return nil
}

// convertAudio 处理普通音频文件：直接 ffmpeg 转码
func (a *App) convertAudio(idx, total int, req FileConvertRequest, targetFormat, outputDir string, status *DecryptStatus) error {
	filePath := req.Path
	fileName := filepath.Base(filePath)

	runtime.EventsEmit(a.ctx, "convert:progress", map[string]interface{}{
		"current": idx, "total": total, "fileName": fileName, "status": "transcoding",
	})

	baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	outName := baseName + "." + targetFormat
	outPath := filepath.Join(outputDir, outName)

	srcExt := strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
	if srcExt == targetFormat {
		inputData, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("读取文件失败: %w", err)
		}
		if err := WriteAudioFile(outPath, inputData); err != nil {
			return fmt.Errorf("写入文件失败: %w", err)
		}
	} else {
		tErr := ffmpeg.Transcode(filePath, outPath, targetFormat, func(pct float64) {
			runtime.EventsEmit(a.ctx, "convert:progress", map[string]interface{}{
				"current": idx, "total": total, "fileName": fileName, "status": "transcoding", "transPct": pct,
			})
		})
		if tErr != nil {
			return fmt.Errorf("转码失败: %w", tErr)
		}
	}

	status.Status = "success"
	status.Output = outPath
	status.Title = strings.TrimSuffix(fileName, filepath.Ext(fileName))
	status.Format = targetFormat
	return nil
}

// GetFileSize 获取文件大小（友好格式）
func (a *App) GetFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}
