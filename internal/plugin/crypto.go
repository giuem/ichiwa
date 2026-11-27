package plugin

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"hash"
	"io"
	"strings"
)

// MaxInflateBytes caps what gunzip and inflate return (§8).
const MaxInflateBytes = 4 << 20

// The functions here are host.crypto and host.encoding (§8) on Go values;
// host.go converts JavaScript arguments to them.

func hashFunc(alg string) (func() hash.Hash, error) {
	switch alg {
	case "md5":
		return md5.New, nil
	case "sha1":
		return sha1.New, nil
	case "sha256":
		return sha256.New, nil
	}
	return nil, fmt.Errorf("不支持的摘要算法 %q", alg)
}

func digestHex(alg string, data []byte) (string, error) {
	newHash, err := hashFunc(alg)
	if err != nil {
		return "", err
	}
	h := newHash()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hmacHex(alg string, key, data []byte) (string, error) {
	newHash, err := hashFunc(alg)
	if err != nil {
		return "", err
	}
	m := hmac.New(newHash, key)
	m.Write(data)
	return hex.EncodeToString(m.Sum(nil)), nil
}

type aesOptions struct {
	Mode    string
	Key     []byte
	IV      []byte
	Data    []byte
	Decrypt bool
	Padding string // pkcs7 (default for ecb and cbc) or none
	AAD     []byte
}

func aesCrypt(o aesOptions) ([]byte, error) {
	block, err := aes.NewCipher(o.Key)
	if err != nil {
		return nil, errors.New("AES 密钥必须是 16、24 或 32 字节")
	}
	padding := o.Padding
	if padding == "" {
		padding = "pkcs7"
	}
	if padding != "pkcs7" && padding != "none" {
		return nil, fmt.Errorf("不支持的填充方式 %q", padding)
	}
	bs := block.BlockSize()
	switch o.Mode {
	case "ecb", "cbc":
		if o.Mode == "cbc" && len(o.IV) != bs {
			return nil, errors.New("CBC 模式的 iv 必须是 16 字节")
		}
		data := o.Data
		if !o.Decrypt && padding == "pkcs7" {
			n := bs - len(data)%bs
			data = append(append([]byte(nil), data...), bytes.Repeat([]byte{byte(n)}, n)...)
		}
		if len(data)%bs != 0 {
			return nil, errors.New("数据长度不是 16 的整数倍")
		}
		out := make([]byte, len(data))
		switch {
		case o.Mode == "cbc" && o.Decrypt:
			cipher.NewCBCDecrypter(block, o.IV).CryptBlocks(out, data)
		case o.Mode == "cbc":
			cipher.NewCBCEncrypter(block, o.IV).CryptBlocks(out, data)
		default:
			for i := 0; i < len(data); i += bs {
				if o.Decrypt {
					block.Decrypt(out[i:i+bs], data[i:i+bs])
				} else {
					block.Encrypt(out[i:i+bs], data[i:i+bs])
				}
			}
		}
		if o.Decrypt && padding == "pkcs7" {
			if len(out) == 0 {
				return nil, errors.New("填充不正确")
			}
			n := int(out[len(out)-1])
			if n == 0 || n > bs || n > len(out) {
				return nil, errors.New("填充不正确")
			}
			for _, b := range out[len(out)-n:] {
				if int(b) != n {
					return nil, errors.New("填充不正确")
				}
			}
			out = out[:len(out)-n]
		}
		return out, nil
	case "ctr":
		if len(o.IV) != bs {
			return nil, errors.New("CTR 模式的 iv 必须是 16 字节")
		}
		out := make([]byte, len(o.Data))
		cipher.NewCTR(block, o.IV).XORKeyStream(out, o.Data)
		return out, nil
	case "gcm":
		if len(o.IV) == 0 {
			return nil, errors.New("GCM 模式需要 iv")
		}
		g, err := cipher.NewGCMWithNonceSize(block, len(o.IV))
		if err != nil {
			return nil, err
		}
		if o.Decrypt {
			out, err := g.Open(nil, o.IV, o.Data, o.AAD)
			if err != nil {
				return nil, errors.New("GCM 解密或校验失败")
			}
			return out, nil
		}
		return g.Seal(nil, o.IV, o.Data, o.AAD), nil
	}
	return nil, fmt.Errorf("不支持的 AES 模式 %q", o.Mode)
}

// rsaEncrypt encrypts data with a public key in PEM (PKIX or PKCS #1) or
// in bare base64 DER.
func rsaEncrypt(publicKey string, data []byte, padding string) ([]byte, error) {
	der := []byte(nil)
	if block, _ := pem.Decode([]byte(publicKey)); block != nil {
		der = block.Bytes
	} else {
		clean := strings.Map(func(r rune) rune {
			if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
				return -1
			}
			return r
		}, publicKey)
		var err error
		if der, err = decodeBase64(clean); err != nil {
			return nil, errors.New("公钥不是 PEM 或 base64")
		}
	}
	var pub *rsa.PublicKey
	if k, err := x509.ParsePKIXPublicKey(der); err == nil {
		rk, ok := k.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("公钥不是 RSA 公钥")
		}
		pub = rk
	} else if k, err := x509.ParsePKCS1PublicKey(der); err == nil {
		pub = k
	} else {
		return nil, errors.New("无法解析公钥")
	}
	switch padding {
	case "pkcs1":
		return rsa.EncryptPKCS1v15(rand.Reader, pub, data)
	case "oaep-sha1":
		return rsa.EncryptOAEP(sha1.New(), rand.Reader, pub, data, nil)
	case "oaep-sha256":
		return rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, data, nil)
	}
	return nil, fmt.Errorf("不支持的 RSA 填充 %q", padding)
}

func randomBytes(n int) ([]byte, error) {
	if n < 0 || n > 1024 {
		return nil, errors.New("randomBytes 的 n 必须在 0 到 1024 之间")
	}
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b, nil
}

func uuid4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// decodeBase64 accepts standard and URL-safe base64, padded or not.
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	s = strings.TrimRight(s, "=")
	return base64.RawStdEncoding.DecodeString(s)
}

func readLimited(r io.Reader) ([]byte, error) {
	out, err := io.ReadAll(io.LimitReader(r, MaxInflateBytes+1))
	if err != nil {
		return nil, err
	}
	if len(out) > MaxInflateBytes {
		return nil, fmt.Errorf("解压结果超过 %d MiB", MaxInflateBytes>>20)
	}
	return out, nil
}

func gunzip(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return readLimited(zr)
}

func inflate(data []byte, raw bool) ([]byte, error) {
	if raw {
		fr := flate.NewReader(bytes.NewReader(data))
		defer fr.Close()
		return readLimited(fr)
	}
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return readLimited(zr)
}
