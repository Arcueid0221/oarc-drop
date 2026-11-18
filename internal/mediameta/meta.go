package mediameta

import (
	"bytes"
	"encoding/binary"
	"path"
	"strings"
	"unicode/utf16"
)

type TrackMetadata struct {
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Album       string  `json:"album"`
	Format      string  `json:"format"`
	SampleRate  int     `json:"sampleRate,omitempty"`
	BitsPerSamp int     `json:"bitsPerSample,omitempty"`
	DurationSec float64 `json:"durationSec,omitempty"`
	HasCover    bool    `json:"hasCover"`
	CoverMime   string  `json:"-"`
	CoverBytes  []byte  `json:"-"`
}

func ParseAudioHeader(fileName string, head []byte) TrackMetadata {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(fileName), "."))
	base := strings.TrimSuffix(path.Base(fileName), path.Ext(fileName))
	meta := TrackMetadata{
		Title:  base,
		Format: strings.ToUpper(ext),
	}
	if len(head) >= 4 && string(head[:4]) == "fLaC" {
		meta.Format = "FLAC"
		parseFLAC(head, &meta)
		return meta
	}
	if len(head) >= 10 && string(head[:3]) == "ID3" {
		parseID3v2(head, &meta)
		return meta
	}
	return meta
}

func parseFLAC(data []byte, meta *TrackMetadata) {
	pos := 4
	for pos+4 <= len(data) {
		header := data[pos]
		isLast := (header & 0x80) != 0
		blockType := header & 0x7F
		blockLen := int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
		pos += 4
		if blockLen < 0 || pos+blockLen > len(data) {
			return
		}
		block := data[pos : pos+blockLen]
		pos += blockLen

		switch blockType {
		case 0: // STREAMINFO
			if len(block) >= 18 {
				// bytes 10..17: 20 bits sample rate, 3 bits channels-1, 5 bits bps-1, 36 bits total samples
				v := binary.BigEndian.Uint64(block[10:18])
				sr := int((v >> 44) & 0xFFFFF)
				bps := int(((v >> 36) & 0x1F) + 1)
				totalSamples := v & 0xFFFFFFFFF
				meta.SampleRate = sr
				meta.BitsPerSamp = bps
				if sr > 0 && totalSamples > 0 {
					meta.DurationSec = float64(totalSamples) / float64(sr)
				}
			}
		case 4: // VORBIS_COMMENT
			parseVorbisComment(block, meta)
		case 6: // PICTURE
			if !meta.HasCover {
				parseFLACPicture(block, meta)
			}
		}
		if isLast {
			break
		}
	}
}

func parseVorbisComment(block []byte, meta *TrackMetadata) {
	if len(block) < 8 {
		return
	}
	vendorLen := int(binary.LittleEndian.Uint32(block[:4]))
	p := 4 + vendorLen
	if vendorLen < 0 || p+4 > len(block) {
		return
	}
	count := int(binary.LittleEndian.Uint32(block[p : p+4]))
	p += 4
	for i := 0; i < count && p+4 <= len(block); i++ {
		cLen := int(binary.LittleEndian.Uint32(block[p : p+4]))
		p += 4
		if cLen < 0 || p+cLen > len(block) {
			return
		}
		kv := string(block[p : p+cLen])
		p += cLen
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			k := strings.ToUpper(strings.TrimSpace(kv[:eq]))
			v := strings.TrimSpace(kv[eq+1:])
			if v == "" {
				continue
			}
			switch k {
			case "TITLE":
				meta.Title = v
			case "ARTIST", "ALBUMARTIST":
				if meta.Artist == "" || k == "ARTIST" {
					meta.Artist = v
				}
			case "ALBUM":
				meta.Album = v
			}
		}
	}
}

func parseFLACPicture(block []byte, meta *TrackMetadata) {
	if len(block) < 32 {
		return
	}
	p := 4 // skip picture type
	if p+4 > len(block) {
		return
	}
	mimeLen := int(binary.BigEndian.Uint32(block[p : p+4]))
	p += 4
	if mimeLen < 0 || p+mimeLen+4 > len(block) {
		return
	}
	mime := string(block[p : p+mimeLen])
	p += mimeLen
	descLen := int(binary.BigEndian.Uint32(block[p : p+4]))
	p += 4
	if descLen < 0 || p+descLen+20 > len(block) {
		return
	}
	p += descLen + 16 // skip width, height, depth, colors
	dataLen := int(binary.BigEndian.Uint32(block[p : p+4]))
	p += 4
	if dataLen <= 0 || p+dataLen > len(block) {
		return
	}
	meta.HasCover = true
	if mime == "" {
		mime = "image/jpeg"
	}
	meta.CoverMime = mime
	meta.CoverBytes = append([]byte(nil), block[p:p+dataLen]...)
}

func parseID3v2(data []byte, meta *TrackMetadata) {
	version := data[3]
	flags := data[5]
	tagSize := synchsafeInt(data[6:10])
	end := 10 + tagSize
	if end > len(data) {
		end = len(data)
	}
	p := 10
	if (flags & 0x40) != 0 { // extended header
		if p+4 > end {
			return
		}
		extSize := int(binary.BigEndian.Uint32(data[p : p+4]))
		if version == 4 {
			extSize = synchsafeInt(data[p : p+4])
		}
		if extSize <= 0 {
			return
		}
		p += extSize
	}

	for p+10 <= end {
		frameID := string(data[p : p+4])
		if frameID[0] == 0 {
			break
		}
		var fSize int
		if version == 4 {
			fSize = synchsafeInt(data[p+4 : p+8])
		} else {
			fSize = int(binary.BigEndian.Uint32(data[p+4 : p+8]))
		}
		p += 10
		if fSize <= 0 || p+fSize > end {
			break
		}
		frame := data[p : p+fSize]
		p += fSize

		switch frameID {
		case "TIT2":
			if s := decodeID3Text(frame); s != "" {
				meta.Title = s
			}
		case "TPE1", "TPE2":
			if s := decodeID3Text(frame); s != "" && (meta.Artist == "" || frameID == "TPE1") {
				meta.Artist = s
			}
		case "TALB":
			if s := decodeID3Text(frame); s != "" {
				meta.Album = s
			}
		case "APIC":
			if !meta.HasCover {
				parseID3APIC(frame, meta)
			}
		}
	}
}

func parseID3APIC(frame []byte, meta *TrackMetadata) {
	if len(frame) < 8 {
		return
	}
	enc := frame[0]
	rest := frame[1:]
	nul := bytes.IndexByte(rest, 0)
	if nul < 0 || nul+2 >= len(rest) {
		return
	}
	mime := string(rest[:nul])
	rest = rest[nul+2:] // skip nul + picture type byte
	// skip description string
	if enc == 1 || enc == 2 {
		// UTF-16 double-nul terminated
		idx := -1
		for i := 0; i+1 < len(rest); i += 2 {
			if rest[i] == 0 && rest[i+1] == 0 {
				idx = i + 2
				break
			}
		}
		if idx < 0 || idx >= len(rest) {
			return
		}
		rest = rest[idx:]
	} else {
		idx := bytes.IndexByte(rest, 0)
		if idx < 0 || idx+1 >= len(rest) {
			return
		}
		rest = rest[idx+1:]
	}
	if len(rest) < 16 {
		return
	}
	if mime == "" || mime == "image/" {
		mime = "image/jpeg"
		if bytes.HasPrefix(rest, []byte{0x89, 'P', 'N', 'G'}) {
			mime = "image/png"
		}
	}
	meta.HasCover = true
	meta.CoverMime = mime
	meta.CoverBytes = append([]byte(nil), rest...)
}

func decodeID3Text(frame []byte) string {
	if len(frame) < 2 {
		return ""
	}
	enc := frame[0]
	b := frame[1:]
	switch enc {
	case 0, 3: // ISO-8859-1 or UTF-8
		return strings.TrimSpace(strings.TrimRight(string(b), "\x00"))
	case 1: // UTF-16 with BOM
		if len(b) < 2 {
			return ""
		}
		le := true
		if b[0] == 0xFE && b[1] == 0xFF {
			le = false
			b = b[2:]
		} else if b[0] == 0xFF && b[1] == 0xFE {
			b = b[2:]
		}
		return decodeUTF16(b, le)
	case 2: // UTF-16BE without BOM
		return decodeUTF16(b, false)
	default:
		return strings.TrimSpace(strings.TrimRight(string(b), "\x00"))
	}
}

func decodeUTF16(b []byte, le bool) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		var v uint16
		if le {
			v = binary.LittleEndian.Uint16(b[i : i+2])
		} else {
			v = binary.BigEndian.Uint16(b[i : i+2])
		}
		if v == 0 {
			break
		}
		u = append(u, v)
	}
	return strings.TrimSpace(string(utf16.Decode(u)))
}

func synchsafeInt(b []byte) int {
	if len(b) < 4 {
		return 0
	}
	return (int(b[0]&0x7F) << 21) | (int(b[1]&0x7F) << 14) | (int(b[2]&0x7F) << 7) | int(b[3]&0x7F)
}
