package cenc

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func mkbox(typ string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	out := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(out, uint32(8+len(body)))
	copy(out[4:], typ)
	return append(out, body...)
}

func u32(v ...uint32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		binary.BigEndian.PutUint32(out[4*i:], x)
	}
	return out
}

func u16(v uint16) []byte { return []byte{byte(v >> 8), byte(v)} }

// sample is one sample in the clear and how it is split into subsamples:
// pairs of clear and protected byte counts, none for a whole-sample one.
type sample struct {
	data []byte
	subs [][2]int
}

// buildMP4 lays out a progressive MP4 of one track with the samples given,
// encrypted with key as the cenc scheme does, its sample entry named
// entry ("encv" or "enca") over the original format orig.
func buildMP4(t *testing.T, key []byte, entry, orig string, samples []sample) (enc, clear []byte) {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	var mdat, clearMdat []byte
	var sizes []uint32
	senc := u32(0x2, uint32(len(samples))) // flags: subsamples present
	if len(samples[0].subs) == 0 {
		senc = u32(0, uint32(len(samples)))
	}
	for i, s := range samples {
		iv := make([]byte, 16)
		binary.BigEndian.PutUint64(iv, uint64(0x1000+i))
		enc := append([]byte(nil), s.data...)
		stream := cipher.NewCTR(block, iv)
		senc = append(senc, iv[:8]...)
		if len(s.subs) == 0 {
			stream.XORKeyStream(enc, enc)
		} else {
			senc = append(senc, u16(uint16(len(s.subs)))...)
			pos := 0
			for _, sub := range s.subs {
				senc = append(senc, u16(uint16(sub[0]))...)
				senc = append(senc, u32(uint32(sub[1]))...)
				pos += sub[0]
				stream.XORKeyStream(enc[pos:pos+sub[1]], enc[pos:pos+sub[1]])
				pos += sub[1]
			}
		}
		mdat = append(mdat, enc...)
		clearMdat = append(clearMdat, s.data...)
		sizes = append(sizes, uint32(len(s.data)))
	}

	sinf := mkbox("sinf",
		mkbox("frma", []byte(orig)),
		mkbox("schm", u32(0), []byte("cenc"), u32(0x10000)),
		mkbox("schi", mkbox("tenc", u32(0), []byte{0, 0, 1, 8}, make([]byte, 16))),
	)
	var head []byte
	if entry == "encv" {
		head = make([]byte, 78)
	} else {
		head = make([]byte, 28)
	}
	stsd := mkbox("stsd", u32(0, 1), mkbox(entry, head, sinf))
	stsz := mkbox("stsz", u32(0, 0, uint32(len(sizes))), u32(sizes...))
	// Two chunks: the first sample alone, then the rest.
	stsc := mkbox("stsc", u32(0, 2), u32(1, 1, 1), u32(2, uint32(len(samples)-1), 1))
	build := func(mdatAt uint32) []byte {
		stco := mkbox("stco", u32(0, 2), u32(mdatAt+8, mdatAt+8+sizes[0]))
		stbl := mkbox("stbl", stsd, stsz, stsc, stco, mkbox("senc", senc), mkbox("saiz", u32(0)), mkbox("saio", u32(0)))
		moov := mkbox("moov", mkbox("trak", mkbox("mdia", mkbox("minf", stbl))))
		return append(mkbox("ftyp", []byte("isom")), moov...)
	}
	head0 := build(0)
	file := build(uint32(len(head0)))
	enc = append(file, mkbox("mdat", mdat)...)
	clear = append(append([]byte(nil), file...), mkbox("mdat", clearMdat)...)
	return enc, clear
}

func samplesOf(n int, subs [][2]int) []sample {
	out := make([]sample, n)
	for i := range out {
		size := 0
		for _, s := range subs {
			size += s[0] + s[1]
		}
		if size == 0 {
			size = 37 + i*5
		}
		data := make([]byte, size)
		for j := range data {
			data[j] = byte(i*31 + j)
		}
		out[i] = sample{data, subs}
	}
	return out
}

// Decrypt restores every protected byte of video samples split into
// subsamples, and leaves the clear bytes between them alone.
func TestDecryptSubsamples(t *testing.T) {
	key, _ := hex.DecodeString("53891e0a11528165bac48415cdc614c2")
	enc, clear := buildMP4(t, key, "encv", "hvc1", samplesOf(4, [][2]int{{5, 32}, {3, 17}}))
	if !IsEncrypted(enc) {
		t.Fatal("IsEncrypted = false for a protected file")
	}
	if err := Decrypt(enc, hex.EncodeToString(key)); err != nil {
		t.Fatal(err)
	}
	if IsEncrypted(enc) {
		t.Fatal("still encrypted after Decrypt")
	}
	at := bytes.Index(enc, []byte("mdat"))
	if !bytes.Equal(enc[at:], clear[at:]) {
		t.Fatal("samples differ from the clear ones")
	}
	if !bytes.Contains(enc, []byte("hvc1")) || bytes.Contains(enc, []byte("sinf")) || bytes.Contains(enc, []byte("senc")) {
		t.Fatal("the sample entry still looks protected")
	}
	if len(enc) != len(clear) {
		t.Fatal("decrypting changed the file's size")
	}
}

// Decrypt handles audio samples encrypted whole, without subsamples.
func TestDecryptWholeSamples(t *testing.T) {
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	enc, clear := buildMP4(t, key, "enca", "mp4a", samplesOf(3, nil))
	if err := Decrypt(enc, hex.EncodeToString(key)); err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(enc, []byte("mdat"))
	if !bytes.Equal(enc[at:], clear[at:]) {
		t.Fatal("samples differ from the clear ones")
	}
	if !bytes.Contains(enc, []byte("mp4a")) {
		t.Fatal("the original format was not restored")
	}
}

// A key that isn't 16 bytes of hex is refused before anything changes.
func TestDecryptBadKey(t *testing.T) {
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	enc, _ := buildMP4(t, key, "encv", "avc1", samplesOf(2, [][2]int{{4, 16}}))
	before := append([]byte(nil), enc...)
	if err := Decrypt(enc, "abc"); err == nil {
		t.Fatal("Decrypt accepted a short key")
	}
	if !bytes.Equal(enc, before) {
		t.Fatal("a refused key changed the file")
	}
}
