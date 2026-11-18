package server_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"oarc-drop/internal/mediameta"
	"oarc-drop/internal/s3client"
	"oarc-drop/internal/server"
	"oarc-drop/internal/store"
)

type memObj struct {
	data        []byte
	contentType string
}

type handlerRoundTripper struct {
	h http.Handler
}

func (rt handlerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rt.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func newFakeSiloTransport(t *testing.T, bucket string, initial map[string]memObj) http.RoundTripper {
	t.Helper()
	var mu sync.RWMutex
	objs := make(map[string]memObj)
	for k, v := range initial {
		objs[k] = v
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			http.Error(w, "missing sigv4", http.StatusForbidden)
			return
		}
		trimmed := strings.TrimPrefix(r.URL.Path, "/"+bucket)
		key := strings.TrimPrefix(trimmed, "/")

		if r.Method == http.MethodGet && key == "" && r.URL.Query().Get("list-type") == "2" {
			mu.RLock()
			defer mu.RUnlock()
			type item struct {
				Key          string `xml:"Key"`
				LastModified string `xml:"LastModified"`
				ETag         string `xml:"ETag"`
				Size         int64  `xml:"Size"`
			}
			var resp struct {
				XMLName     xml.Name `xml:"ListBucketResult"`
				IsTruncated bool     `xml:"IsTruncated"`
				Contents    []item   `xml:"Contents"`
			}
			for k, o := range objs {
				resp.Contents = append(resp.Contents, item{
					Key:          k,
					LastModified: "2026-11-18T06:00:00Z",
					ETag:         "\"etag123\"",
					Size:         int64(len(o.data)),
				})
			}
			w.Header().Set("Content-Type", "application/xml")
			_ = xml.NewEncoder(w).Encode(resp)
			return
		}

		switch r.Method {
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			objs[key] = memObj{data: b, contentType: r.Header.Get("Content-Type")}
			mu.Unlock()
			w.Header().Set("ETag", "\"put-etag\"")
			w.WriteHeader(http.StatusOK)

		case http.MethodGet:
			mu.RLock()
			o, ok := objs[key]
			mu.RUnlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			if o.contentType != "" {
				w.Header().Set("Content-Type", o.contentType)
			}
			http.ServeContent(w, r, filepath.Base(key), time.Time{}, bytes.NewReader(o.data))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return handlerRoundTripper{h: h}
}

func buildID3MP3Sample(title, artist, album string, coverJPEG []byte) []byte {
	var frames bytes.Buffer
	writeTextFrame := func(id, val string) {
		payload := append([]byte{3}, []byte(val)...) // 3 = UTF-8
		frames.WriteString(id)
		var sz [4]byte
		binary.BigEndian.PutUint32(sz[:], uint32(len(payload)))
		frames.Write(sz[:])
		frames.Write([]byte{0, 0})
		frames.Write(payload)
	}
	writeTextFrame("TIT2", title)
	writeTextFrame("TPE1", artist)
	writeTextFrame("TALB", album)

	if len(coverJPEG) > 0 {
		var apic bytes.Buffer
		apic.WriteByte(0)
		apic.WriteString("image/jpeg")
		apic.WriteByte(0)
		apic.WriteByte(3)
		apic.WriteByte(0)
		apic.Write(coverJPEG)

		frames.WriteString("APIC")
		var sz [4]byte
		binary.BigEndian.PutUint32(sz[:], uint32(apic.Len()))
		frames.Write(sz[:])
		frames.Write([]byte{0, 0})
		frames.Write(apic.Bytes())
	}

	raw := frames.Bytes()
	n := len(raw)
	synch := []byte{
		byte((n >> 21) & 0x7F),
		byte((n >> 14) & 0x7F),
		byte((n >> 7) & 0x7F),
		byte(n & 0x7F),
	}
	var out bytes.Buffer
	out.WriteString("ID3")
	out.Write([]byte{3, 0, 0})
	out.Write(synch)
	out.Write(raw)
	out.Write([]byte{0xFF, 0xFB, 0x90, 0x64})
	return out.Bytes()
}

func TestEndToEndFlow(t *testing.T) {
	fakeCover := []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	mp3Bytes := buildID3MP3Sample("Tsukihime Theme", "TYPE-MOON", "Blue Glass Moon", fakeCover)

	meta := mediameta.ParseAudioHeader("theme.mp3", mp3Bytes)
	if meta.Title != "Tsukihime Theme" || meta.Artist != "TYPE-MOON" || !meta.HasCover {
		t.Fatalf("unexpected parsed audio meta: %+v", meta)
	}

	siloTR := newFakeSiloTransport(t, "aurora", map[string]memObj{
		"aurora/photos/pic1.jpg":        {data: []byte("full-jpg-bytes"), contentType: "image/jpeg"},
		"aurora/photos/thumbs/pic1.jpg": {data: []byte("thumb-jpg"), contentType: "image/jpeg"},
		"aurora/music/theme.mp3":        {data: mp3Bytes, contentType: "audio/mpeg"},
		"aurora/music/theme.lrc":        {data: []byte("[00:01.00] Hello Moon\n"), contentType: "text/plain"},
	})

	s3c, err := s3client.New(s3client.Config{
		Endpoint:  "http://minio:9000",
		AccessKey: "minioadmin",
		SecretKey: "secret123",
		Bucket:    "aurora",
		PublicURL: "https://img.arcueidblog.top",
		Transport: siloTR,
	})
	if err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	fakeGDriveRoot := filepath.Join(tmpDir, "fake-gdrive")
	_ = os.MkdirAll(fakeGDriveRoot, 0o755)
	fakeRclone := filepath.Join(tmpDir, "fake-rclone.sh")
	rcloneScript := fmt.Sprintf(`#!/bin/sh
set -e
cmd="$1"
if [ "$cmd" = "copyto" ]; then
  src="$2"
  dst=$(echo "$3" | sed 's|^gdrive:OArc-Drop/|%s/|')
  mkdir -p "$(dirname "$dst")"
  cp "$src" "$dst"
elif [ "$cmd" = "cat" ]; then
  src=$(echo "$2" | sed 's|^gdrive:OArc-Drop/|%s/|')
  cat "$src"
fi
`, fakeGDriveRoot, fakeGDriveRoot)
	if err := os.WriteFile(fakeRclone, []byte(rcloneScript), 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := store.New(filepath.Join(tmpDir, "data"), 50)
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(server.Config{
		DataDir:         filepath.Join(tmpDir, "data"),
		S3:              s3c,
		GDriveRemoteDir: "gdrive:OArc-Drop",
		RcloneBin:       fakeRclone,
		RequireCFAccess: true,
	}, st)

	doReq := func(method, target string, body io.Reader, ctype string, admin bool, cookies []*http.Cookie) *http.Response {
		t.Helper()
		req := httptest.NewRequest(method, "http://drop.local"+target, body)
		if admin {
			req.Header.Set("Cf-Access-Authenticated-User-Email", "owner@arcueidblog.top")
		}
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		for _, ck := range cookies {
			req.AddCookie(ck)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Result()
	}

	// 1. Unauthorized /api/* without Cf-Access header must return 401
	res := doReq(http.MethodGet, "/api/status", nil, "", false, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without CF Access header, got %d", res.StatusCode)
	}

	// 2. Silo Media Index: thumbs/ deduplicated, paired thumbUrl, audio + lyrics indexed
	mResp := doReq(http.MethodGet, "/api/media", nil, "", true, nil)
	var mData struct {
		Items      []server.MediaItem `json:"items"`
		ImageCount int                `json:"imageCount"`
		AudioCount int                `json:"audioCount"`
	}
	_ = json.NewDecoder(mResp.Body).Decode(&mData)
	if mData.ImageCount != 1 || mData.AudioCount != 1 {
		t.Fatalf("expected 1 image (thumbs deduplicated) and 1 audio, got img=%d audio=%d", mData.ImageCount, mData.AudioCount)
	}

	// 3. Track metadata + embedded cover + .lrc lyrics
	tmResp := doReq(http.MethodGet, "/api/media/track-meta?key=aurora/music/theme.mp3", nil, "", true, nil)
	var tmData struct {
		Meta     mediameta.TrackMetadata `json:"meta"`
		CoverURL string                  `json:"coverUrl"`
		Lyrics   string                  `json:"lyrics"`
	}
	_ = json.NewDecoder(tmResp.Body).Decode(&tmData)
	if tmData.Meta.Title != "Tsukihime Theme" || tmData.CoverURL == "" || !strings.Contains(tmData.Lyrics, "Hello Moon") {
		t.Fatalf("unexpected track-meta response: %+v", tmData)
	}

	// 4. Lossless PNG Clipboard Paste + 1-click public share
	var mpBuf bytes.Buffer
	mw := multipart.NewWriter(&mpBuf)
	fw, _ := mw.CreateFormFile("file", "screen.png")
	rawPNG := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 99, 88, 77}
	_, _ = fw.Write(rawPNG)
	mw.Close()
	cResp := doReq(http.MethodPost, "/api/clips/file", &mpBuf, mw.FormDataContentType(), true, nil)
	var clip store.ClipItem
	_ = json.NewDecoder(cResp.Body).Decode(&clip)
	if clip.Kind != "image" || clip.Size != int64(len(rawPNG)) {
		t.Fatalf("unexpected clip: %+v", clip)
	}

	// 5. Chunked Upload -> Silo target (also updates media index!)
	siloSong := buildID3MP3Sample("Arcueid Waltz", "O.Arc", "Luminary", fakeCover)
	sInit := doReq(http.MethodPost, "/api/uploads/init", strings.NewReader(fmt.Sprintf(`{"fileName":"waltz.mp3","size":%d,"target":"silo","siloPrefix":"aurora/music/"}`, len(siloSong))), "application/json", true, nil)
	var sInitRes struct {
		UploadID string `json:"uploadId"`
	}
	_ = json.NewDecoder(sInit.Body).Decode(&sInitRes)
	doReq(http.MethodPut, "/api/uploads/"+sInitRes.UploadID+"/part/1", bytes.NewReader(siloSong), "application/octet-stream", true, nil)
	sDone := doReq(http.MethodPost, "/api/uploads/"+sInitRes.UploadID+"/complete", nil, "", true, nil)
	if sDone.StatusCode != http.StatusCreated {
		t.Fatalf("silo upload complete status %d", sDone.StatusCode)
	}
	mResp2 := doReq(http.MethodGet, "/api/media", nil, "", true, nil)
	_ = json.NewDecoder(mResp2.Body).Decode(&mData)
	if mData.AudioCount != 2 {
		t.Fatalf("expected 2 audio items after Silo upload, got %d", mData.AudioCount)
	}

	// 6. Chunked Upload -> Google Drive target + Passcode-protected public share (/s/{code}/* without CF Access!)
	payload := bytes.Repeat([]byte("lossless-gdrive-data-"), 500)
	initBody := fmt.Sprintf(`{"fileName":"archive.tar.zst","size":%d,"target":"gdrive","passcode":"0221","ttlHours":24}`, len(payload))
	uInit := doReq(http.MethodPost, "/api/uploads/init", strings.NewReader(initBody), "application/json", true, nil)
	var initRes struct {
		UploadID string `json:"uploadId"`
	}
	_ = json.NewDecoder(uInit.Body).Decode(&initRes)
	doReq(http.MethodPut, "/api/uploads/"+initRes.UploadID+"/part/1", bytes.NewReader(payload), "application/octet-stream", true, nil)
	uDone := doReq(http.MethodPost, "/api/uploads/"+initRes.UploadID+"/complete", nil, "", true, nil)
	var doneRes struct {
		Share store.ShareItem `json:"share"`
	}
	_ = json.NewDecoder(uDone.Body).Decode(&doneRes)
	if doneRes.Share.Code == "" || !doneRes.Share.HasPasscode {
		t.Fatalf("unexpected completed share: %+v", doneRes.Share)
	}

	pubForbidden := doReq(http.MethodGet, "/s/"+doneRes.Share.Code+"/raw", nil, "", false, nil)
	if pubForbidden.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 before passcode verify, got %d", pubForbidden.StatusCode)
	}

	vResp := doReq(http.MethodPost, "/s/"+doneRes.Share.Code+"/verify", strings.NewReader(`{"passcode":"0221"}`), "application/json", false, nil)
	cookies := vResp.Cookies()
	if vResp.StatusCode != http.StatusOK || len(cookies) == 0 {
		t.Fatalf("verify failed: status=%d", vResp.StatusCode)
	}

	dlResp := doReq(http.MethodGet, "/s/"+doneRes.Share.Code+"/raw?download=1", nil, "", false, cookies)
	gotBytes, _ := io.ReadAll(dlResp.Body)
	if !bytes.Equal(gotBytes, payload) {
		t.Fatalf("downloaded bytes mismatch: got %d want %d", len(gotBytes), len(payload))
	}
}
