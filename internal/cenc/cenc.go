// Package cenc decrypts a progressive (non-fragmented) MP4 protected with
// the CENC "cenc" scheme (AES-128 CTR) in place.
//
// Decryption keeps every box the same size, so no offset changes:
// each encrypted sample is decrypted where it sits in mdat, the protected
// sample entry (encv/enca) takes back its original format from frma, and
// the protection boxes (sinf, senc, saio, saiz, seig sample groups) are
// renamed to "free" so players ignore them.
package cenc

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

type box struct {
	typ   string
	start int // offset of the box header
	body  int // offset of the payload
	end   int
}

func readBoxes(buf []byte, start, end int) ([]box, error) {
	var out []box
	for o := start; o+8 <= end; {
		size := int(binary.BigEndian.Uint32(buf[o:]))
		typ := string(buf[o+4 : o+8])
		hdr := 8
		switch size {
		case 1:
			if o+16 > end {
				return nil, errors.New("cenc: truncated box")
			}
			size = int(binary.BigEndian.Uint64(buf[o+8:]))
			hdr = 16
		case 0:
			size = end - o
		}
		if size < hdr || o+size > end {
			return nil, fmt.Errorf("cenc: bad %q box size", typ)
		}
		out = append(out, box{typ: typ, start: o, body: o + hdr, end: o + size})
		o += size
	}
	return out, nil
}

func child(buf []byte, b box, typ string) (box, bool) {
	kids, err := readBoxes(buf, b.body, b.end)
	if err != nil {
		return box{}, false
	}
	for _, k := range kids {
		if k.typ == typ {
			return k, true
		}
	}
	return box{}, false
}

func path(buf []byte, b box, types ...string) (box, bool) {
	cur := b
	for _, t := range types {
		var ok bool
		if cur, ok = child(buf, cur, t); !ok {
			return box{}, false
		}
	}
	return cur, true
}

func rename(buf []byte, b box, typ string) { copy(buf[b.start+4:b.start+8], typ) }

// IsEncrypted reports whether the MP4 has a protected sample entry.
func IsEncrypted(buf []byte) bool {
	top, err := readBoxes(buf, 0, len(buf))
	if err != nil {
		return false
	}
	for _, b := range top {
		if b.typ != "moov" {
			continue
		}
		traks, _ := readBoxes(buf, b.body, b.end)
		for _, t := range traks {
			if t.typ != "trak" {
				continue
			}
			if stsd, ok := path(buf, t, "mdia", "minf", "stbl", "stsd"); ok && stsd.body+16 <= stsd.end {
				typ := string(buf[stsd.body+12 : stsd.body+16])
				if typ == "encv" || typ == "enca" {
					return true
				}
			}
		}
	}
	return false
}

// Decrypt decrypts buf in place with the 16-byte key given as hex.
func Decrypt(buf []byte, keyHex string) error {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 16 {
		return errors.New("cenc: key must be 32 hex characters")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	top, err := readBoxes(buf, 0, len(buf))
	if err != nil {
		return err
	}
	var moov box
	found := false
	for _, b := range top {
		if b.typ == "moov" {
			moov, found = b, true
		}
		if b.typ == "moof" {
			return errors.New("cenc: fragmented MP4 is not supported")
		}
	}
	if !found {
		return errors.New("cenc: no moov box")
	}
	traks, err := readBoxes(buf, moov.body, moov.end)
	if err != nil {
		return err
	}
	for _, t := range traks {
		if t.typ != "trak" {
			continue
		}
		if err := decryptTrack(buf, t, block); err != nil {
			return err
		}
	}
	return nil
}

func decryptTrack(buf []byte, trak box, block cipher.Block) error {
	stbl, ok := path(buf, trak, "mdia", "minf", "stbl")
	if !ok {
		return nil
	}
	stsd, ok := child(buf, stbl, "stsd")
	if !ok || stsd.body+8 > stsd.end {
		return nil
	}
	// stsd: fullbox header (4) + entry_count (4), then sample entries.
	entries, err := readBoxes(buf, stsd.body+8, stsd.end)
	if err != nil || len(entries) == 0 {
		return err
	}
	entry := entries[0]
	var childStart int
	switch entry.typ {
	case "encv":
		childStart = entry.body + 78
	case "enca":
		childStart = entry.body + 28
		// QuickTime sound sample description version 1/2 carries more fields.
		if v := binary.BigEndian.Uint16(buf[entry.body+8:]); v == 1 {
			childStart += 16
		} else if v == 2 {
			childStart += 36
		}
	default:
		return nil // clear track
	}
	kids, err := readBoxes(buf, childStart, entry.end)
	if err != nil {
		return err
	}
	var sinf box
	for _, k := range kids {
		if k.typ == "sinf" {
			sinf = k
		}
	}
	if sinf.typ == "" {
		return errors.New("cenc: protected entry without sinf")
	}
	frma, ok := child(buf, sinf, "frma")
	if !ok || frma.body+4 > frma.end {
		return errors.New("cenc: missing frma")
	}
	if schm, ok := child(buf, sinf, "schm"); ok && schm.body+8 <= schm.end {
		if s := string(buf[schm.body+4 : schm.body+8]); s != "cenc" {
			return fmt.Errorf("cenc: unsupported scheme %q", s)
		}
	}
	tenc, ok := path(buf, sinf, "schi", "tenc")
	if !ok || tenc.body+24 > tenc.end {
		return errors.New("cenc: missing tenc")
	}
	// tenc: fullbox(4) reserved(1) byte(1) isProtected(1) ivSize(1) kid(16) [constIVSize constIV]
	defaultIVSize := int(buf[tenc.body+7])
	var constIV []byte
	if defaultIVSize == 0 && tenc.body+25 <= tenc.end {
		n := int(buf[tenc.body+24])
		if tenc.body+25+n <= tenc.end {
			constIV = buf[tenc.body+25 : tenc.body+25+n]
		}
	}

	offsets, sizes, err := sampleTable(buf, stbl)
	if err != nil {
		return err
	}

	senc, ok := child(buf, stbl, "senc")
	if !ok {
		return errors.New("cenc: missing senc (sample auxiliary info)")
	}
	p := senc.body
	flags := binary.BigEndian.Uint32(buf[p:]) & 0xffffff
	count := int(binary.BigEndian.Uint32(buf[p+4:]))
	p += 8
	if count > len(sizes) {
		return fmt.Errorf("cenc: senc has %d samples, track has %d", count, len(sizes))
	}
	ivSize := defaultIVSize
	iv := make([]byte, 16)
	for i := 0; i < count; i++ {
		clear(iv)
		if ivSize > 0 {
			if p+ivSize > senc.end {
				return errors.New("cenc: truncated senc")
			}
			copy(iv, buf[p:p+ivSize])
			p += ivSize
		} else {
			copy(iv, constIV)
		}
		off, size := offsets[i], sizes[i]
		if off+size > len(buf) {
			return errors.New("cenc: sample outside file")
		}
		stream := cipher.NewCTR(block, iv)
		sample := buf[off : off+size]
		if flags&0x2 != 0 {
			if p+2 > senc.end {
				return errors.New("cenc: truncated senc")
			}
			n := int(binary.BigEndian.Uint16(buf[p:]))
			p += 2
			pos := 0
			for j := 0; j < n; j++ {
				if p+6 > senc.end {
					return errors.New("cenc: truncated senc")
				}
				clearBytes := int(binary.BigEndian.Uint16(buf[p:]))
				protBytes := int(binary.BigEndian.Uint32(buf[p+2:]))
				p += 6
				pos += clearBytes
				if pos+protBytes > len(sample) {
					return errors.New("cenc: subsample outside sample")
				}
				stream.XORKeyStream(sample[pos:pos+protBytes], sample[pos:pos+protBytes])
				pos += protBytes
			}
		} else {
			stream.XORKeyStream(sample, sample)
		}
	}

	// Make the track look clear.
	copy(buf[entry.start+4:entry.start+8], buf[frma.body:frma.body+4])
	rename(buf, sinf, "free")
	stblKids, _ := readBoxes(buf, stbl.body, stbl.end)
	for _, k := range stblKids {
		switch k.typ {
		case "senc", "saio", "saiz":
			rename(buf, k, "free")
		case "sgpd", "sbgp":
			// grouping_type follows the fullbox header.
			if k.body+8 <= k.end && string(buf[k.body+4:k.body+8]) == "seig" {
				rename(buf, k, "free")
			}
		}
	}
	return nil
}

// sampleTable returns the file offset and size of every sample of a track.
func sampleTable(buf []byte, stbl box) (offsets, sizes []int, err error) {
	stsz, ok := child(buf, stbl, "stsz")
	if !ok {
		return nil, nil, errors.New("cenc: missing stsz")
	}
	uniform := int(binary.BigEndian.Uint32(buf[stsz.body+4:]))
	n := int(binary.BigEndian.Uint32(buf[stsz.body+8:]))
	sizes = make([]int, n)
	for i := range sizes {
		if uniform != 0 {
			sizes[i] = uniform
		} else {
			sizes[i] = int(binary.BigEndian.Uint32(buf[stsz.body+12+4*i:]))
		}
	}

	var chunks []int
	if stco, ok := child(buf, stbl, "stco"); ok {
		c := int(binary.BigEndian.Uint32(buf[stco.body+4:]))
		for i := 0; i < c; i++ {
			chunks = append(chunks, int(binary.BigEndian.Uint32(buf[stco.body+8+4*i:])))
		}
	} else if co64, ok := child(buf, stbl, "co64"); ok {
		c := int(binary.BigEndian.Uint32(buf[co64.body+4:]))
		for i := 0; i < c; i++ {
			chunks = append(chunks, int(binary.BigEndian.Uint64(buf[co64.body+8+8*i:])))
		}
	} else {
		return nil, nil, errors.New("cenc: missing stco")
	}

	stsc, ok := child(buf, stbl, "stsc")
	if !ok {
		return nil, nil, errors.New("cenc: missing stsc")
	}
	type run struct{ first, per int }
	ec := int(binary.BigEndian.Uint32(buf[stsc.body+4:]))
	runs := make([]run, ec)
	for i := range runs {
		b := stsc.body + 8 + 12*i
		runs[i] = run{int(binary.BigEndian.Uint32(buf[b:])), int(binary.BigEndian.Uint32(buf[b+4:]))}
	}

	offsets = make([]int, 0, n)
	r := 0
	for ci, chunkOff := range chunks {
		for r+1 < len(runs) && runs[r+1].first <= ci+1 {
			r++
		}
		if len(runs) == 0 {
			break
		}
		o := chunkOff
		for k := 0; k < runs[r].per && len(offsets) < n; k++ {
			offsets = append(offsets, o)
			o += sizes[len(offsets)-1]
		}
	}
	if len(offsets) != n {
		return nil, nil, fmt.Errorf("cenc: sample table maps %d of %d samples", len(offsets), n)
	}
	return offsets, sizes, nil
}
