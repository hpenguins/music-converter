package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup 应用启动时调用
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// 注册文件拖拽事件
	runtime.OnFileDrop(ctx, func(x, y int, paths []string) {
		if len(paths) == 0 {
			return
		}
		// 将拖拽的文件路径通过事件发送给前端
		runtime.EventsEmit(ctx, "wails:dragdrop", paths)
	})
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
	Status    string `json:"status"` // "pending", "processing", "success", "error"
	Progress  int    `json:"progress"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	Title     string `json:"title,omitempty"`
	Artist    string `json:"artist,omitempty"`
	Album     string `json:"album,omitempty"`
	Format    string `json:"format,omitempty"`
	CoverPath string `json:"coverPath,omitempty"`
}

// SelectNCMFiles 打开文件选择对话框，仅选择 .ncm 文件
func (a *App) SelectNCMFiles() ([]FileInfo, error) {
	files, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择 NCM 文件",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "网易云音乐文件 (*.ncm)",
				Pattern:     "*.ncm",
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

	// 排序：按文件名
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

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


// DecryptFiles 批量解密 NCM 文件
func (a *App) DecryptFiles(files []string, outputDir string) ([]DecryptStatus, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("没有选择文件")
	}

	// 确保输出目录存在
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("创建输出目录失败: %w", err)
	}

	results := make([]DecryptStatus, len(files))

	for i, filePath := range files {
		fileName := filepath.Base(filePath)
		results[i] = DecryptStatus{
			FileName: fileName,
			FilePath: filePath,
			Status:   "processing",
		}

		// 发送进度事件
		runtime.EventsEmit(a.ctx, "decrypt:progress", map[string]interface{}{
			"current":  i,
			"total":    len(files),
			"fileName": fileName,
			"status":   "processing",
		})

		// 解密
		result, audioData, err := DecryptToBuffer(filePath)
		if err != nil {
			results[i].Status = "error"
			results[i].Error = err.Error()

			runtime.EventsEmit(a.ctx, "decrypt:progress", map[string]interface{}{
				"current":  i + 1,
				"total":    len(files),
				"fileName": fileName,
				"status":   "error",
				"error":    err.Error(),
			})
			continue
		}

		// 生成输出文件名
		outName := fileName[:len(fileName)-4] + "." + result.Format
		outPath := filepath.Join(outputDir, outName)

		// 写入音频文件
		if err := WriteAudioFile(outPath, audioData); err != nil {
			results[i].Status = "error"
			results[i].Error = fmt.Sprintf("写入文件失败: %v", err)

			runtime.EventsEmit(a.ctx, "decrypt:progress", map[string]interface{}{
				"current":  i + 1,
				"total":    len(files),
				"fileName": fileName,
				"status":   "error",
				"error":    err.Error(),
			})
			continue
		}

		// 写入封面图片
		coverPath := ""
		if result.CoverData != nil {
			coverPath = WriteCoverFile(result.CoverData, outPath)
		}

		results[i].Status = "success"
		results[i].Output = outPath
		results[i].Title = result.Title
		results[i].Artist = result.Artist
		results[i].Album = result.Album
		results[i].Format = result.Format
		results[i].CoverPath = coverPath

		runtime.EventsEmit(a.ctx, "decrypt:progress", map[string]interface{}{
			"current":   i + 1,
			"total":     len(files),
			"fileName":  fileName,
			"status":    "success",
			"output":    outPath,
			"title":     result.Title,
			"artist":    result.Artist,
			"album":     result.Album,
			"format":    result.Format,
			"coverPath": coverPath,
		})
	}

	return results, nil
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
