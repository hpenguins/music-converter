package main

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NCM 常量
var (
	ncmMagic = []byte("CTENFDAM")
	coreKey  []byte
	metaKey  []byte
)

func init() {
	var err error
	coreKey, err = hex.DecodeString("687a4852416d736f356b496e62617857")
	if err != nil {
		panic("core key hex decode failed: " + err.Error())
	}
	metaKey, err = hex.DecodeString("2331346C6A6B5F215C5D2630553C2728")
	if err != nil {
		panic("meta key hex decode failed: " + err.Error())
	}
}

// NCMResult 存储解密结果
type NCMResult struct {
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	Format     string `json:"format"`
	OutputPath string `json:"outputPath"`
	CoverData  []byte `json:"-"`
}

// pkcs7Unpad 去除 PKCS7 填充
func pkcs7Unpad(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	padLen := int(data[len(data)-1])
	if padLen < 1 || padLen > 16 {
		return data
	}
	if padLen > len(data) {
		return data
	}
	for _, b := range data[len(data)-padLen:] {
		if b != byte(padLen) {
			return data
		}
	}
	return data[:len(data)-padLen]
}

// createKeyBox 生成 Key Box (RC4-like 256字节密钥盒)
func createKeyBox(keyData []byte) []byte {
	S := make([]byte, 256)
	for i := 0; i < 256; i++ {
		S[i] = byte(i)
	}
	keyLen := len(keyData)
	j := byte(0)
	for i := 0; i < 256; i++ {
		j = (j + S[i] + keyData[i%keyLen]) & 0xFF
		S[i], S[j] = S[j], S[i]
	}
	result := make([]byte, 256)
	for idx := 0; idx < 256; idx++ {
		t := byte((idx + 1) & 0xFF)
		i := S[t]
		n := S[(t+i)&0xFF]
		result[idx] = S[(i+n)&0xFF]
	}
	return result
}

// guessAudioFormat 根据音频文件头部特征猜测格式
func guessAudioFormat(audioData []byte) string {
	if len(audioData) < 4 {
		return "mp3"
	}
	if bytes.HasPrefix(audioData, []byte{0xFF, 0xFB}) ||
		bytes.HasPrefix(audioData, []byte{0xFF, 0xF3}) ||
		bytes.HasPrefix(audioData, []byte{0xFF, 0xF2}) {
		return "mp3"
	}
	if bytes.HasPrefix(audioData, []byte("fLaC")) {
		return "flac"
	}
	if bytes.HasPrefix(audioData, []byte("OggS")) {
		return "ogg"
	}
	if bytes.HasPrefix(audioData, []byte("ftyp")) ||
		(len(audioData) > 7 && bytes.Equal(audioData[4:8], []byte("ftyp"))) {
		return "m4a"
	}
	if bytes.HasPrefix(audioData, []byte("RIFF")) {
		return "wav"
	}
	return "mp3"
}

// tryParseMetaInfo 尝试解析元数据 JSON
func tryParseMetaInfo(metaStr string) map[string]interface{} {
	result := make(map[string]interface{})
	colonIdx := strings.Index(metaStr, ":")
	if colonIdx != -1 {
		metaStr = metaStr[colonIdx+1:]
	}
	if err := json.Unmarshal([]byte(metaStr), &result); err != nil {
		return result
	}
	if mainMusic, ok := result["mainMusic"].(map[string]interface{}); ok {
		result = mainMusic
	}
	return result
}

// extractArtist 从 metaInfo 中提取歌手信息
func extractArtist(metaInfo map[string]interface{}) string {
	a, ok := metaInfo["artist"]
	if !ok {
		return ""
	}
	switch v := a.(type) {
	case []interface{}:
		parts := make([]string, 0)
		for _, item := range v {
			switch arr := item.(type) {
			case []interface{}:
				if len(arr) > 0 {
					if s, ok := arr[0].(string); ok {
						parts = append(parts, s)
					}
				}
			case string:
				parts = append(parts, arr)
			}
		}
		return strings.Join(parts, "; ")
	case string:
		return v
	}
	return ""
}

// decryptNCM 解密 NCM 文件数据
func decryptNCM(data []byte, filename string) (*NCMResult, []byte, error) {
	offset := 0

	if len(data) < 8 {
		return nil, nil, errors.New("文件太小，不是有效的 NCM 文件")
	}
	if !bytes.Equal(data[:8], ncmMagic) {
		return nil, nil, fmt.Errorf("无效的 NCM 文件: 魔数不匹配")
	}
	offset += 10

	if offset >= len(data) {
		return nil, nil, errors.New("文件被截断: 魔数后无数据")
	}

	keyLength := binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	if offset+int(keyLength) > len(data) {
		return nil, nil, errors.New("文件被截断: 密钥数据不完整")
	}

	encryptedKey := make([]byte, keyLength)
	for i := 0; i < int(keyLength); i++ {
		encryptedKey[i] = data[offset+i] ^ 0x64
	}
	offset += int(keyLength)

	block, err := aes.NewCipher(coreKey)
	if err != nil {
		return nil, nil, fmt.Errorf("创建 AES 解密器失败: %w", err)
	}
	decryptedKey := make([]byte, len(encryptedKey))
	newECBDecrypter(block).CryptBlocks(decryptedKey, encryptedKey)
	decryptedKey = pkcs7Unpad(decryptedKey)

	if len(decryptedKey) <= 17 {
		return nil, nil, errors.New("解密后的密钥数据太短")
	}
	keyData := decryptedKey[17:]

	if offset+4 > len(data) {
		return nil, nil, errors.New("文件被截断: 元数据长度字段缺失")
	}
	metaLength := binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	metaInfo := make(map[string]interface{})
	if metaLength > 0 {
		if offset+int(metaLength) > len(data) {
			return nil, nil, errors.New("文件被截断: 元数据不完整")
		}

		encryptedMetaRaw := make([]byte, metaLength)
		for i := 0; i < int(metaLength); i++ {
			encryptedMetaRaw[i] = data[offset+i] ^ 0x63
		}
		offset += int(metaLength)

		if len(encryptedMetaRaw) <= 22 {
			return nil, nil, errors.New("元数据太短")
		}
		metaBase64 := string(encryptedMetaRaw[22:])

		encryptedMeta, err := base64.StdEncoding.DecodeString(metaBase64)
		if err != nil {
			return nil, nil, fmt.Errorf("Base64 解码元数据失败: %w", err)
		}

		metaBlock, err := aes.NewCipher(metaKey)
		if err != nil {
			return nil, nil, fmt.Errorf("创建元数据 AES 解密器失败: %w", err)
		}
		decryptedMeta := make([]byte, len(encryptedMeta))
		newECBDecrypter(metaBlock).CryptBlocks(decryptedMeta, encryptedMeta)
		decryptedMeta = pkcs7Unpad(decryptedMeta)
		metaInfo = tryParseMetaInfo(string(decryptedMeta))
	}

	keyBox := createKeyBox(keyData)

	if offset+9 > len(data) {
		return nil, nil, errors.New("文件被截断: 封面区域缺失")
	}
	imageLength := binary.LittleEndian.Uint32(data[offset+5:])

	var coverData []byte
	if imageLength > 0 {
		if offset+13+int(imageLength) > len(data) {
			return nil, nil, errors.New("文件被截断: 封面数据不完整")
		}
		coverData = data[offset+13 : offset+13+int(imageLength)]
	}

	offset += int(imageLength) + 13

	audioEncrypted := data[offset:]
	audioData := make([]byte, len(audioEncrypted))
	for i := 0; i < len(audioEncrypted); i++ {
		audioData[i] = audioEncrypted[i] ^ keyBox[i&0xFF]
	}

	audioFormat := ""
	if f, ok := metaInfo["format"].(string); ok && f != "" {
		audioFormat = f
	} else {
		audioFormat = guessAudioFormat(audioData)
	}

	title := ""
	if t, ok := metaInfo["musicName"].(string); ok {
		title = t
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(filename), ".ncm")
	}

	artist := extractArtist(metaInfo)

	album := ""
	if al, ok := metaInfo["album"].(string); ok {
		album = al
	}

	result := &NCMResult{
		Title:     title,
		Artist:    artist,
		Album:     album,
		Format:    audioFormat,
		CoverData: coverData,
	}

	return result, audioData, nil
}

// DecryptToBuffer 解密 NCM 文件并返回数据和音频数据
func DecryptToBuffer(inputPath string) (*NCMResult, []byte, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, nil, fmt.Errorf("读取文件失败: %w", err)
	}
	return decryptNCM(data, filepath.Base(inputPath))
}

// WriteAudioFile 写入音频文件
func WriteAudioFile(outputPath string, audioData []byte) error {
	return os.WriteFile(outputPath, audioData, 0644)
}

// WriteCoverFile 写入封面图片（与音频文件同路径，后缀加 _cover）
func WriteCoverFile(coverData []byte, audioPath string) string {
	if coverData == nil {
		return ""
	}
	ext := ".jpg"
	coverPath := strings.TrimSuffix(audioPath, filepath.Ext(audioPath)) + "_cover" + ext
	if err := os.WriteFile(coverPath, coverData, 0644); err != nil {
		return ""
	}
	return coverPath
}

// synchsafe 将 32 位整数编码为 ID3v2 的 synchsafe 整数
func synchsafe(n int) []byte {
	return []byte{
		byte((n >> 21) & 0x7F),
		byte((n >> 14) & 0x7F),
		byte((n >> 7) & 0x7F),
		byte(n & 0x7F),
	}
}

// embedID3v2 为 MP3 文件写入 ID3v2.3 标签（含专辑封面）
func embedID3v2(audioPath string, title, artist, album string, coverData []byte) error {
	allData, err := os.ReadFile(audioPath)
	if err != nil {
		return err
	}

	dataOffset := 0
	if len(allData) > 10 && string(allData[:3]) == "ID3" {
		size := int(allData[6])<<21 | int(allData[7])<<14 | int(allData[8])<<7 | int(allData[9])
		tagEnd := 10 + size
		if tagEnd > 0 && tagEnd <= len(allData) {
			dataOffset = tagEnd
		}
	}
	rawAudio := allData[dataOffset:]

	type id3Frame struct {
		id   string
		data []byte
	}

	encText := func(s string) []byte {
		d := []byte{0x03}
		d = append(d, []byte(s)...)
		return d
	}

	var frames []id3Frame

	if title != "" {
		frames = append(frames, id3Frame{"TIT2", encText(title)})
	}
	if artist != "" {
		frames = append(frames, id3Frame{"TPE1", encText(artist)})
	}
	if album != "" {
		frames = append(frames, id3Frame{"TALB", encText(album)})
	}
	if coverData != nil {
		apic := []byte{0x03}
		apic = append(apic, []byte("image/jpeg")...)
		apic = append(apic, 0x00)
		apic = append(apic, 0x03)
		apic = append(apic, 0x00)
		apic = append(apic, coverData...)
		frames = append(frames, id3Frame{"APIC", apic})
	}

	if len(frames) == 0 {
		return nil
	}

	frameSize := 0
	for _, fr := range frames {
		frameSize += 10 + len(fr.data)
	}

	tagHeader := []byte("ID3")
	tagHeader = append(tagHeader, 0x03, 0x00)
	tagHeader = append(tagHeader, 0x00)
	tagHeader = append(tagHeader, synchsafe(frameSize)...)

	var tagFrames []byte
	for _, fr := range frames {
		fh := []byte(fr.id)
		fh = append(fh, byte(len(fr.data)>>24), byte(len(fr.data)>>16), byte(len(fr.data)>>8), byte(len(fr.data)))
		fh = append(fh, 0x00, 0x00)
		tagFrames = append(tagFrames, fh...)
		tagFrames = append(tagFrames, fr.data...)
	}

	out := append(tagHeader, tagFrames...)
	out = append(out, rawAudio...)
	return os.WriteFile(audioPath, out, 0644)
}

// embedFLACPicture 向 FLAC 文件写入封面图片（METADATA_BLOCK_PICTURE）
func embedFLACPicture(audioPath string, coverData []byte) error {
	if coverData == nil {
		return nil
	}

	data, err := os.ReadFile(audioPath)
	if err != nil {
		return err
	}

	if len(data) < 4 || string(data[:4]) != "fLaC" {
		return fmt.Errorf("不是有效的 FLAC 文件")
	}

	pos := 4
	for pos < len(data) {
		if pos+4 > len(data) {
			return fmt.Errorf("FLAC 文件截断")
		}
		isLast := (data[pos] & 0x80) != 0
		blockLen := int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
		blockEnd := pos + 4 + blockLen
		if blockEnd > len(data) {
			return fmt.Errorf("FLAC 元数据块截断")
		}
		if isLast {
			// 清除原最后标记
			data[pos] &^= 0x80

			// 构建 PICTURE 块
			picData := buildFLACPictureBlock(coverData)
			if picData == nil {
				return nil
			}

			picHeader := []byte{0x80 | 6} // is_last, type=6 PICTURE
			picHeader = append(picHeader, byte(len(picData)>>16), byte(len(picData)>>8), byte(len(picData)))

			out := make([]byte, 0, blockEnd+len(picHeader)+len(picData)+len(data)-blockEnd)
			out = append(out, data[:blockEnd]...)
			out = append(out, picHeader...)
			out = append(out, picData...)
			out = append(out, data[blockEnd:]...)
			return os.WriteFile(audioPath, out, 0644)
		}
		pos += 4 + blockLen
	}
	return fmt.Errorf("FLAC 文件中未找到元数据块结束标记")
}

// buildFLACPictureBlock 构建 FLAC PICTURE 元数据块内容
func buildFLACPictureBlock(coverData []byte) []byte {
	if len(coverData) == 0 {
		return nil
	}

	var b []byte
	// picture type: 3 (front cover)
	b = append(b, 0, 0, 0, 3)
	// mime string length
	mime := "image/jpeg"
	b = append(b, byte(len(mime)>>24), byte(len(mime)>>16), byte(len(mime)>>8), byte(len(mime)))
	b = append(b, []byte(mime)...)
	// description length = 0
	b = append(b, 0, 0, 0, 0)
	// width, height, depth, colors = 0
	b = append(b, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	// picture data length
	b = append(b, byte(len(coverData)>>24), byte(len(coverData)>>16), byte(len(coverData)>>8), byte(len(coverData)))
	b = append(b, coverData...)
	return b
}

// EmbedMetadata 将元数据和封面内嵌到音频文件中
func EmbedMetadata(audioPath, title, artist, album string, coverData []byte) error {
	ext := strings.ToLower(filepath.Ext(audioPath))
	switch ext {
	case ".mp3":
		return embedID3v2(audioPath, title, artist, album, coverData)
	case ".flac":
		return embedFLACPicture(audioPath, coverData)
	default:
		return nil
	}
}
