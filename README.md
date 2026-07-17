# Audio Converter

Convert audio files between formats and decrypt NCM files to standard audio formats (MP3, FLAC, OGG, WAV).

## Features

- Select audio files via file dialog or drag-and-drop
- Decrypt NCM files
- Transcode between audio formats (requires FFmpeg)
- Batch conversion with progress indication
- Choose output directory (defaults to Downloads folder)
- Click a completed file to preview its metadata and album cover
- Settings panel to toggle separate cover file output

## Usage

1. Click "Select Files" or drag audio files onto the window
2. Choose an output directory (or keep the default Downloads folder)
3. Select output format per file (when FFmpeg is available)
4. Click "Start Convert"
5. Click any successfully converted row to view details

## FFmpeg

For audio transcoding, place `ffmpeg.exe` in the application directory or ensure it is available in your PATH. Without FFmpeg, only decryption of NCM files is supported (AUTO format).

## Build

```bash
git clone <repo-url>
cd audio-converter
cd frontend && npm install && cd ..
wails build
```

The executable will be at `build/bin/audio-converter.exe`.

## Requirements

- Go 1.23+
- Node.js 16+
- Wails CLI v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)

## Tech Stack

- **Backend**: Go + Wails v2
- **Frontend**: React 18 + TypeScript + Vite
- **Cryptography**: AES-128-ECB (Go standard library)
