# NCM Converter

Convert NCM files to standard audio formats (MP3, FLAC, OGG, M4A, WAV).

## Features

- Select NCM files via file dialog or drag-and-drop
- Decrypt multiple files in batch with progress indication
- Choose output directory (defaults to Downloads folder)
- Click a completed file to preview its metadata and album cover
- Supports MP3, FLAC, OGG, M4A, WAV output (auto-detected)

## Usage

1. Click "Select NCM Files" or drag `.ncm` files onto the window
2. Choose an output directory (or keep the default Downloads folder)
3. Click "Start Decrypt"
4. Click any successfully decrypted row to view details

## Build

```bash
git clone <repo-url>
cd music-converter

# Install frontend dependencies
cd frontend && npm install && cd ..

# Development mode (hot reload)
wails dev

# Production build
wails build
```

The executable will be at `build/bin/music-converter.exe`.

## Requirements

- Go 1.23+
- Node.js 16+
- Wails CLI v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)

## Tech Stack

- **Backend**: Go + Wails v2
- **Frontend**: React 18 + TypeScript + Vite
- **Cryptography**: AES-128-ECB (Go standard library)
