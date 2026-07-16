package main

import "crypto/cipher"

// Go 标准库不直接提供 ECB 模式，这里手动实现

type ecbDecrypter struct {
	b         cipher.Block
	blockSize int
}

func newECBDecrypter(b cipher.Block) *ecbDecrypter {
	return &ecbDecrypter{b: b, blockSize: b.BlockSize()}
}

func (d *ecbDecrypter) BlockSize() int { return d.blockSize }

func (d *ecbDecrypter) CryptBlocks(dst, src []byte) {
	if len(src)%d.blockSize != 0 {
		panic("crypto/cipher: input not full blocks")
	}
	if len(dst) < len(src) {
		panic("crypto/cipher: output smaller than input")
	}
	for len(src) > 0 {
		d.b.Decrypt(dst, src)
		src = src[d.blockSize:]
		dst = dst[d.blockSize:]
	}
}

type ecbEncrypter struct {
	b         cipher.Block
	blockSize int
}

func newECBEncrypter(b cipher.Block) *ecbEncrypter {
	return &ecbEncrypter{b: b, blockSize: b.BlockSize()}
}

func (d *ecbEncrypter) BlockSize() int { return d.blockSize }

func (d *ecbEncrypter) CryptBlocks(dst, src []byte) {
	if len(src)%d.blockSize != 0 {
		panic("crypto/cipher: input not full blocks")
	}
	if len(dst) < len(src) {
		panic("crypto/cipher: output smaller than input")
	}
	for len(src) > 0 {
		d.b.Encrypt(dst, src)
		src = src[d.blockSize:]
		dst = dst[d.blockSize:]
	}
}
