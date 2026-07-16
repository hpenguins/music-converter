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
	// KSA
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

	// PRGA (JS 映射等效，不改变 S 盒)
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

	// 处理协议前缀: `music:{...}`, `dj:{...}`
	colonIdx := strings.Index(metaStr, ":")
	if colonIdx != -1 {
		metaStr = metaStr[colonIdx+1:]
	}

	if err := json.Unmarshal([]byte(metaStr), &result); err != nil {
		return result
	}

	// 如果是电台歌曲 (dj:)，使用 mainMusic 子结构
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

	// 1. 验证魔数
	if len(data) < 8 {
		return nil, nil, errors.New("文件太小，不是有效的 NCM 文件")
	}
	if !bytes.Equal(data[:8], ncmMagic) {
		return nil, nil, fmt.Errorf("无效的 NCM 文件: 魔数不匹配")
	}
	offset += 10 // 8字节魔数 + 2字节保留区

	if offset >= len(data) {
		return nil, nil, errors.New("文件被截断: 魔数后无数据")
	}

	// 2. 获取并解密 AES 核心密钥
	keyLength := binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	if offset+int(keyLength) > len(data) {
		return nil, nil, errors.New("文件被截断: 密钥数据不完整")
	}

	// XOR 0x64
	encryptedKey := make([]byte, keyLength)
	for i := 0; i < int(keyLength); i++ {
		encryptedKey[i] = data[offset+i] ^ 0x64
	}
	offset += int(keyLength)

	// AES-128-ECB 解密
	block, err := aes.NewCipher(coreKey)
	if err != nil {
		return nil, nil, fmt.Errorf("创建 AES 解密器失败: %w", err)
	}
	decryptedKey := make([]byte, len(encryptedKey))
	newECBDecrypter(block).CryptBlocks(decryptedKey, encryptedKey)
	decryptedKey = pkcs7Unpad(decryptedKey)

	// 截取前 17 字节后的内容作为 Key Box 密钥数据
	if len(decryptedKey) <= 17 {
		return nil, nil, errors.New("解密后的密钥数据太短")
	}
	keyData := decryptedKey[17:]

	// 3. 解析并解密歌曲元数据
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

		// XOR 0x63
		encryptedMetaRaw := make([]byte, metaLength)
		for i := 0; i < int(metaLength); i++ {
			encryptedMetaRaw[i] = data[offset+i] ^ 0x63
		}
		offset += int(metaLength)

		// 截取 22 字节之后的部分并进行 Base64 解码
		if len(encryptedMetaRaw) <= 22 {
			return nil, nil, errors.New("元数据太短")
		}
		metaBase64 := string(encryptedMetaRaw[22:])

		encryptedMeta, err := base64.StdEncoding.DecodeString(metaBase64)
		if err != nil {
			return nil, nil, fmt.Errorf("Base64 解码元数据失败: %w", err)
		}

		// AES-128-ECB 解密元数据（使用 metaKey）
		metaBlock, err := aes.NewCipher(metaKey)
		if err != nil {
			return nil, nil, fmt.Errorf("创建元数据 AES 解密器失败: %w", err)
		}
		decryptedMeta := make([]byte, len(encryptedMeta))
		newECBDecrypter(metaBlock).CryptBlocks(decryptedMeta, encryptedMeta)
		decryptedMeta = pkcs7Unpad(decryptedMeta)
		metaInfo = tryParseMetaInfo(string(decryptedMeta))
	}

	// 4. 创建 Key Box
	keyBox := createKeyBox(keyData)

	// 5. 提取专辑封面图
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

	// 6. 解密音频数据
	audioEncrypted := data[offset:]
	audioData := make([]byte, len(audioEncrypted))
	for i := 0; i < len(audioEncrypted); i++ {
		audioData[i] = audioEncrypted[i] ^ keyBox[i&0xFF]
	}

	// 7. 获取音频格式
	audioFormat := ""
	if f, ok := metaInfo["format"].(string); ok && f != "" {
		audioFormat = f
	} else {
		audioFormat = guessAudioFormat(audioData)
	}

	// 8. 提取元数据字段
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

// synchsafe 将 32 位整数编码为 ID3v2 的 synchsafe 整数（每字节只用 7 位）
func synchsafe(n int) []byte {
	return []byte{
		byte((n >> 21) & 0x7F),
		byte((n >> 14) & 0x7F),
		byte((n >> 7) & 0x7F),
		byte(n & 0x7F),
	}
}

// embedID3v2 为 MP3 文件写入 ID3v2.3 标签（含专辑封面）
// 读取整个文件 → 剥离已有 ID3v2 标签 → 写入新标签头+帧 → 追加纯音频数据
func embedID3v2(audioPath string, title, artist, album string, coverData []byte) error {
	// 1. 读取整个文件
	allData, err := os.ReadFile(audioPath)
	if err != nil {
		return err
	}

	// 2. 剥离已有 ID3v2 标签（前 10 字节头 + synchsafe 字段声明的尺寸）
	dataOffset := 0
	if len(allData) > 10 && string(allData[:3]) == "ID3" {
		size := int(allData[6])<<21 | int(allData[7])<<14 | int(allData[8])<<7 | int(allData[9])
		tagEnd := 10 + size
		if tagEnd > 0 && tagEnd <= len(allData) {
			dataOffset = tagEnd
		}
	}
	rawAudio := allData[dataOffset:]

	// 3. 构建帧列表
	type id3Frame struct {
		id   string
		data []byte
	}

	encText := func(s string) []byte {
		d := []byte{0x03} // UTF-8 编码字节
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
		// APIC 帧: encoding(1) + mime(N + \0) + picType(1) + desc(\0) + data
		apic := []byte{0x03} // UTF-8
		apic = append(apic, []byte("image/jpeg")...)
		apic = append(apic, 0x00) // null terminator
		apic = append(apic, 0x03) // front cover
		apic = append(apic, 0x00) // empty description
		apic = append(apic, coverData...)
		frames = append(frames, id3Frame{"APIC", apic})
	}

	if len(frames) == 0 {
		return nil // 无标签可写，不修改文件
	}

	// 4. 计算 ID3v2 标签总大小（帧头 10 + 帧数据）
	frameSize := 0
	for _, fr := range frames {
		frameSize += 10 + len(fr.data)
	}

	// 5. 构建 ID3v2 头: "ID3" + ver(3,0) + flags(0) + synchsafe size
	tagHeader := []byte("ID3")
	tagHeader = append(tagHeader, 0x03, 0x00) // v2.3
	tagHeader = append(tagHeader, 0x00)       // flags
	tagHeader = append(tagHeader, synchsafe(frameSize)...)

	// 6. 构建完整标签帧
	var tagFrames []byte
	for _, fr := range frames {
		// 帧头: id(4) + size(4) + flags(2)
		fh := []byte(fr.id)
		fh = append(fh, byte(len(fr.data)>>24), byte(len(fr.data)>>16), byte(len(fr.data)>>8), byte(len(fr.data)))
		fh = append(fh, 0x00, 0x00) // no flags
		tagFrames = append(tagFrames, fh...)
		tagFrames = append(tagFrames, fr.data...)
	}

	// 7. 以截断方式写回文件：标签头 + 帧数据 + 纯音频
	out := append(tagHeader, tagFrames...)
	out = append(out, rawAudio...)
	return os.WriteFile(audioPath, out, 0644)
}

// EmbedMetadata 将元数据和封面内嵌到音频文件中
func EmbedMetadata(audioPath, title, artist, album string, coverData []byte) error {
	ext := strings.ToLower(filepath.Ext(audioPath))
	switch ext {
	case ".mp3":
		return embedID3v2(audioPath, title, artist, album, coverData)
	case ".flac":
		// FLAC 元数据块嵌入较复杂，暂用外部文件替代
		// 可后续扩展
		return nil
	default:
		return nil
	}
}
