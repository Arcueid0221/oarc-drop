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
	"net/url"
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
	mpParts := make(map[string]map[int][]byte)
	mpTypes := make(map[string]string)

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
			prefix := r.URL.Query().Get("prefix")
			for k, o := range objs {
				if prefix != "" && !strings.HasPrefix(k, prefix) {
					continue
				}
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

		// S3 Multipart Upload endpoints
		if r.Method == http.MethodPost && r.URL.Query().Has("uploads") {
			uid := fmt.Sprintf("mp-%d", time.Now().UnixNano())
			mu.Lock()
			mpParts[uid] = make(map[int][]byte)
			mpTypes[uid] = r.Header.Get("Content-Type")
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "<InitiateMultipartUploadResult><UploadId>%s</UploadId></InitiateMultipartUploadResult>", uid)
			return
		}
		if uid := r.URL.Query().Get("uploadId"); uid != "" {
			switch r.Method {
			case http.MethodPut:
				var pNum int
				_, _ = fmt.Sscanf(r.URL.Query().Get("partNumber"), "%d", &pNum)
				b, _ := io.ReadAll(r.Body)
				mu.Lock()
				if mpParts[uid] != nil {
					mpParts[uid][pNum] = b
				}
				mu.Unlock()
				w.Header().Set("ETag", fmt.Sprintf("\"etag-part-%d\"", pNum))
				w.WriteHeader(http.StatusOK)
				return
			case http.MethodPost:
				mu.Lock()
				partsMap := mpParts[uid]
				ct := mpTypes[uid]
				var full bytes.Buffer
				for i := 1; i <= len(partsMap); i++ {
					full.Write(partsMap[i])
				}
				objs[key] = memObj{data: full.Bytes(), contentType: ct}
				delete(mpParts, uid)
				delete(mpTypes, uid)
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("<CompleteMultipartUploadResult><ETag>\"mp-done\"</ETag></CompleteMultipartUploadResult>"))
				return
			case http.MethodDelete:
				mu.Lock()
				delete(mpParts, uid)
				delete(mpTypes, uid)
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		switch r.Method {
		case http.MethodHead:
			mu.RLock()
			o, ok := objs[key]
			mu.RUnlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(o.data)))
			w.WriteHeader(http.StatusOK)

		case http.MethodPut:
			if cpSrc := r.Header.Get("X-Amz-Copy-Source"); cpSrc != "" {
				decodedSrc, _ := url.PathUnescape(strings.TrimPrefix(cpSrc, "/"+bucket+"/"))
				mu.Lock()
				srcObj, ok := objs[decodedSrc]
				if !ok {
					mu.Unlock()
					http.NotFound(w, r)
					return
				}
				objs[key] = srcObj
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("<CopyObjectResult><ETag>\"copy\"</ETag></CopyObjectResult>"))
				return
			}
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			objs[key] = memObj{data: b, contentType: r.Header.Get("Content-Type")}
			mu.Unlock()
			w.Header().Set("ETag", "\"put-etag\"")
			w.WriteHeader(http.StatusOK)

		case http.MethodDelete:
			mu.Lock()
			delete(objs, key)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)

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
		"photos/pic1.jpg":                         {data: []byte("full-jpg-bytes"), contentType: "image/jpeg"},
		"photos/thumbs/pic1.jpg":                  {data: []byte("thumb-jpg"), contentType: "image/jpeg"},
		"music/audio/TYPE-MOON - Tsukihime.mp3":   {data: mp3Bytes, contentType: "audio/mpeg"},
		"music/lyrics/TYPE-MOON - Tsukihime.lrc":  {data: []byte("[00:01.00] Hello Moon\n"), contentType: "text/plain"},
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
	rcloneScript := strings.ReplaceAll(`#!/bin/sh
set -e
cmd="$1"
if [ "$cmd" = "copyto" ]; then
  src=$(echo "$2" | sed 's|^gdrive:OArc-Drop/|__GDRIVE_ROOT__/|')
  dst=$(echo "$3" | sed 's|^gdrive:OArc-Drop/|__GDRIVE_ROOT__/|')
  mkdir -p "$(dirname "$dst")"
  cp "$src" "$dst"
elif [ "$cmd" = "moveto" ]; then
  src=$(echo "$2" | sed 's|^gdrive:OArc-Drop/|__GDRIVE_ROOT__/|')
  dst=$(echo "$3" | sed 's|^gdrive:OArc-Drop/|__GDRIVE_ROOT__/|')
  mkdir -p "$(dirname "$dst")"
  mv "$src" "$dst"
elif [ "$cmd" = "cat" ]; then
  src=$(echo "$2" | sed 's|^gdrive:OArc-Drop/|__GDRIVE_ROOT__/|')
  cat "$src"
elif [ "$cmd" = "mkdir" ]; then
  dst=$(echo "$2" | sed 's|^gdrive:OArc-Drop/|__GDRIVE_ROOT__/|')
  mkdir -p "$dst"
elif [ "$cmd" = "deletefile" ]; then
  dst=$(echo "$2" | sed 's|^gdrive:OArc-Drop/|__GDRIVE_ROOT__/|')
  rm -f "$dst"
elif [ "$cmd" = "lsf" ]; then
  dir=$(echo "$3" | sed 's|^gdrive:OArc-Drop$|__GDRIVE_ROOT__|; s|^gdrive:OArc-Drop/|__GDRIVE_ROOT__/|')
  if [ -d "$dir" ]; then
    for f in "$dir"/*; do
      [ -f "$f" ] && basename "$f"
    done
  fi
elif [ "$cmd" = "purge" ]; then
  dst=$(echo "$2" | sed 's|^gdrive:OArc-Drop/|__GDRIVE_ROOT__/|')
  rm -rf "$dst"
elif [ "$cmd" = "lsjson" ]; then
  dir=$(echo "$2" | sed 's|^gdrive:OArc-Drop$|__GDRIVE_ROOT__|; s|^gdrive:OArc-Drop/|__GDRIVE_ROOT__/|')
  if [ "$4" = "--dirs-only" ]; then
    printf '[{"Path":"2026-11"},{"Path":"归档"}]'
  elif [ "$4" = "--files-only" ]; then
    if [ -d "$dir" ]; then
      first=1
      printf '['
      for f in "$dir"/*; do
        if [ -f "$f" ]; then
          [ "$first" = "0" ] && printf ','
          first=0
          bn=$(basename "$f")
          sz=$(wc -c < "$f" | tr -d ' ')
          printf '{"Path":"%s","Name":"%s","Size":%s,"MimeType":"application/octet-stream","ModTime":"2026-11-19T00:00:00Z"}' "$bn" "$bn" "$sz"
        fi
      done
      printf ']'
    else
      printf '[]'
    fi
  else
    printf '[{"Path":"2026-11"},{"Path":"归档"}]'
  fi
fi
`, "__GDRIVE_ROOT__", fakeGDriveRoot)
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

	// 3. Track metadata + embedded cover + cross-folder .lrc lyrics
	tmResp := doReq(http.MethodGet, "/api/media/track-meta?key="+url.QueryEscape("music/audio/TYPE-MOON - Tsukihime.mp3"), nil, "", true, nil)
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
	sInit := doReq(http.MethodPost, "/api/uploads/init", strings.NewReader(fmt.Sprintf(`{"fileName":"waltz.mp3","size":%d,"target":"silo","siloPrefix":"music/audio/"}`, len(siloSong))), "application/json", true, nil)
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

	// 7. Image-Host Paste -> Silo (clean naming + collision -2 + direct private clip stream)
	uploadToImgHost := func(fname string) store.ClipItem {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		_ = w.WriteField("target", "silo")
		_ = w.WriteField("prefix", "images/")
		part, _ := w.CreateFormFile("file", fname)
		_, _ = part.Write(rawPNG)
		w.Close()
		resp := doReq(http.MethodPost, "/api/clips/file", &b, w.FormDataContentType(), true, nil)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201 for imghost paste, got %d", resp.StatusCode)
		}
		var item store.ClipItem
		_ = json.NewDecoder(resp.Body).Decode(&item)
		return item
	}
	img1 := uploadToImgHost("arch.png")
	if img1.StorageKey != "images/arch.png" || img1.PublicURL != "https://img.arcueidblog.top/aurora/images/arch.png" {
		t.Fatalf("unexpected clean imghost key/url: %+v", img1)
	}
	img2 := uploadToImgHost("arch.png")
	if img2.StorageKey != "images/arch-2.png" {
		t.Fatalf("expected collision suffix arch-2.png, got %s", img2.StorageKey)
	}

	// 8. Silo Vault protection lock + rename + delete
	delProtected := doReq(http.MethodDelete, "/api/silo/object?key=photos/pic1.jpg", nil, "", true, nil)
	if delProtected.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 deleting protected blog photo without force, got %d", delProtected.StatusCode)
	}
	renRes := doReq(http.MethodPost, "/api/silo/rename", strings.NewReader(`{"key":"images/arch-2.png","newName":"diagram.png"}`), "application/json", true, nil)
	if renRes.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 renaming images/arch-2.png, got %d", renRes.StatusCode)
	}
	delUnprotected := doReq(http.MethodDelete, "/api/silo/object?key=images/diagram.png", nil, "", true, nil)
	if delUnprotected.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 deleting images/diagram.png, got %d", delUnprotected.StatusCode)
	}

	// 9. Zero-dependency QR Code SVG endpoint
	qrResp := doReq(http.MethodGet, "/api/qr?text="+url.QueryEscape("https://drop.arcueidblog.top/s/test123"), nil, "", true, nil)
	qrSvg, _ := io.ReadAll(qrResp.Body)
	if qrResp.StatusCode != http.StatusOK || !bytes.Contains(qrSvg, []byte("<svg")) {
		t.Fatalf("unexpected qr response: status=%d len=%d", qrResp.StatusCode, len(qrSvg))
	}

	// 10. Drive folders + per-file promotion of private clip (`clip`) to Silo image host & Google Drive folder
	mkDirResp := doReq(http.MethodPost, "/api/gdrive/folders", strings.NewReader(`{"folder":"归档"}`), "application/json", true, nil)
	if mkDirResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating Drive folder, got %d", mkDirResp.StatusCode)
	}
	lsDirResp := doReq(http.MethodGet, "/api/gdrive/folders", nil, "", true, nil)
	var lsDirs struct {
		Folders []string `json:"folders"`
	}
	_ = json.NewDecoder(lsDirResp.Body).Decode(&lsDirs)
	if len(lsDirs.Folders) < 2 || lsDirs.Folders[0] != "/" {
		t.Fatalf("unexpected Drive folders response: %+v", lsDirs.Folders)
	}

	promResp := doReq(http.MethodPost, "/api/clips/"+clip.ID+"/silo", strings.NewReader(`{"prefix":"images/"}`), "application/json", true, nil)
	var promClip store.ClipItem
	_ = json.NewDecoder(promResp.Body).Decode(&promClip)
	if promResp.StatusCode != http.StatusOK || promClip.PublicURL != "https://img.arcueidblog.top/aurora/images/screen.png" {
		t.Fatalf("expected clip promoted to Silo images/screen.png, got status=%d clip=%+v", promResp.StatusCode, promClip)
	}

	gdResp := doReq(http.MethodPost, "/api/clips/"+clip.ID+"/gdrive", strings.NewReader(`{"folder":"归档"}`), "application/json", true, nil)
	var gdClip store.ClipItem
	_ = json.NewDecoder(gdResp.Body).Decode(&gdClip)
	if gdResp.StatusCode != http.StatusOK || gdClip.GDrivePath != "归档/screen.png" {
		t.Fatalf("expected clip archived to Google Drive 归档/screen.png, got status=%d clip=%+v", gdResp.StatusCode, gdClip)
	}

	// 11. Duplicate archive to same Google Drive folder must auto-suffix (-2) instead of overwriting!
	gdResp2 := doReq(http.MethodPost, "/api/clips/"+clip.ID+"/gdrive", strings.NewReader(`{"folder":"归档"}`), "application/json", true, nil)
	var gdClip2 store.ClipItem
	_ = json.NewDecoder(gdResp2.Body).Decode(&gdClip2)
	if gdResp2.StatusCode != http.StatusOK || gdClip2.GDrivePath != "归档/screen-2.png" {
		t.Fatalf("expected collision-safe Drive path 归档/screen-2.png, got status=%d clip=%+v", gdResp2.StatusCode, gdClip2)
	}

	// 12. Repeated generic `image.png` pastes get distinct timestamped filenames
	pasteGeneric := func() store.ClipItem {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		part, _ := w.CreateFormFile("file", "image.png")
		_, _ = part.Write(rawPNG)
		w.Close()
		r := doReq(http.MethodPost, "/api/clips/file", &b, w.FormDataContentType(), true, nil)
		var it store.ClipItem
		_ = json.NewDecoder(r.Body).Decode(&it)
		return it
	}
	p1 := pasteGeneric()
	p2 := pasteGeneric()
	if !strings.HasPrefix(p1.FileName, "paste-") || p1.FileName == p2.FileName {
		t.Fatalf("expected distinct timestamped paste filenames, got %q and %q", p1.FileName, p2.FileName)
	}

	// 13. Rename clip preserves extension + 1-shot burn-after-reading share does NOT burn on HEAD or ?preview=1
	patchClipResp := doReq(http.MethodPatch, "/api/clips/"+p1.ID, strings.NewReader(`{"pinned":true,"fileName":"speedtest"}`), "application/json", true, nil)
	var patchedClip store.ClipItem
	_ = json.NewDecoder(patchClipResp.Body).Decode(&patchedClip)
	if !patchedClip.Pinned || patchedClip.FileName != "speedtest.png" {
		t.Fatalf("expected pinned speedtest.png, got %+v", patchedClip)
	}

	shCreateResp := doReq(http.MethodPost, "/api/clips/"+p1.ID+"/share", strings.NewReader(`{"ttlHours":24,"maxDownloads":5}`), "application/json", true, nil)
	var createdSh store.ShareItem
	_ = json.NewDecoder(shCreateResp.Body).Decode(&createdSh)
	// Patch share to 1-time download + passcode
	patchShResp := doReq(http.MethodPatch, "/api/shares/"+createdSh.Code, strings.NewReader(`{"maxDownloads":1,"passcode":"9999"}`), "application/json", true, nil)
	if patchShResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 patching share, got %d", patchShResp.StatusCode)
	}
	v9 := doReq(http.MethodPost, "/s/"+createdSh.Code+"/verify", strings.NewReader(`{"passcode":"9999"}`), "application/json", false, nil)
	ck9 := v9.Cookies()

	// HEAD probe & inline ?preview=1 must NOT consume the single download quota
	if hr := doReq(http.MethodHead, "/s/"+createdSh.Code+"/raw?download=1", nil, "", false, ck9); hr.StatusCode != http.StatusOK {
		t.Fatalf("HEAD probe failed: %d", hr.StatusCode)
	}
	if pr := doReq(http.MethodGet, "/s/"+createdSh.Code+"/raw?preview=1", nil, "", false, ck9); pr.StatusCode != http.StatusOK {
		t.Fatalf("inline preview failed: %d", pr.StatusCode)
	}
	// Real download #1 succeeds, download #2 returns 410 Gone
	if d1 := doReq(http.MethodGet, "/s/"+createdSh.Code+"/raw?download=1", nil, "", false, ck9); d1.StatusCode != http.StatusOK {
		t.Fatalf("real download #1 should succeed, got %d", d1.StatusCode)
	}
	if d2 := doReq(http.MethodGet, "/s/"+createdSh.Code+"/raw?download=1", nil, "", false, ck9); d2.StatusCode != http.StatusGone {
		t.Fatalf("download #2 after maxDownloads=1 should return 410, got %d", d2.StatusCode)
	}

	// 14. Cross-tab sync: renaming Silo object updates ClipItem.PublicURL; clearing unpinned keeps pinned clip
	renScreen := doReq(http.MethodPost, "/api/silo/rename", strings.NewReader(`{"key":"images/screen.png","newName":"hero-banner.png"}`), "application/json", true, nil)
	if renScreen.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 renaming images/screen.png, got %d", renScreen.StatusCode)
	}
	updatedOriginClip, _ := st.GetClip(clip.ID)
	if updatedOriginClip.PublicURL != "https://img.arcueidblog.top/aurora/images/hero-banner.png" {
		t.Fatalf("expected clip PublicURL synced after Silo rename, got %s", updatedOriginClip.PublicURL)
	}

	clrResp := doReq(http.MethodPost, "/api/clips/clear-unpinned", nil, "", true, nil)
	if clrResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 clearing unpinned clips, got %d", clrResp.StatusCode)
	}
	remaining := st.ListClips()
	if len(remaining) != 1 || remaining[0].ID != p1.ID {
		t.Fatalf("expected only 1 pinned clip remaining, got %d", len(remaining))
	}

	// 15. Unauthenticated PWA full-screen manifest & iOS/Android PNG icons under /assets/*
	mfResp := doReq(http.MethodGet, "/assets/manifest.webmanifest", nil, "", false, nil)
	if mfResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for manifest without auth, got %d", mfResp.StatusCode)
	}
	for _, iconPath := range []string{
		"/assets/apple-touch-icon.png",
		"/assets/icon-192.png",
		"/assets/icon-512.png",
		"/assets/icon-512-maskable.png",
	} {
		icResp := doReq(http.MethodGet, iconPath, nil, "", false, nil)
		icBytes, _ := io.ReadAll(icResp.Body)
		if icResp.StatusCode != http.StatusOK || len(icBytes) < 1024 || !bytes.HasPrefix(icBytes, []byte{0x89, 'P', 'N', 'G'}) {
			t.Fatalf("expected valid PNG icon %s without auth, status=%d len=%d", iconPath, icResp.StatusCode, len(icBytes))
		}
	}

	// 16. Silo & Drive folder creation + cross-pool specific folder copy + protected/non-empty folder deletion guards
	mkSiloDir := doReq(http.MethodPost, "/api/silo/folders", strings.NewReader(`{"folder":"images/memes"}`), "application/json", true, nil)
	if mkSiloDir.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating Silo folder images/memes, got %d", mkSiloDir.StatusCode)
	}
	vObjsResp := doReq(http.MethodGet, "/api/silo/objects", nil, "", true, nil)
	var vObjs struct {
		Items   []server.VaultObject `json:"items"`
		Folders []string             `json:"folders"`
	}
	_ = json.NewDecoder(vObjsResp.Body).Decode(&vObjs)
	hasMemesFolder := false
	for _, f := range vObjs.Folders {
		if f == "images/memes" {
			hasMemesFolder = true
		}
	}
	for _, it := range vObjs.Items {
		if it.Name == ".keep" {
			t.Fatalf(".keep marker must not leak into Vault file items")
		}
	}
	if !hasMemesFolder {
		t.Fatalf("expected empty Silo folder images/memes persisted in folders list: %+v", vObjs.Folders)
	}

	// Copy Silo photos/pic1.jpg -> Silo images/memes/ & Google Drive 归档/
	cpSiloToSilo := doReq(http.MethodPost, "/api/vault/copy", strings.NewReader(`{"srcStorage":"silo","srcKey":"photos/pic1.jpg","dstStorage":"silo","dstFolder":"images/memes"}`), "application/json", true, nil)
	if cpSiloToSilo.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 copying Silo -> Silo folder, got %d", cpSiloToSilo.StatusCode)
	}
	cpSiloToDrive := doReq(http.MethodPost, "/api/vault/copy", strings.NewReader(`{"srcStorage":"silo","srcKey":"photos/pic1.jpg","dstStorage":"gdrive","dstFolder":"归档"}`), "application/json", true, nil)
	if cpSiloToDrive.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 copying Silo -> Drive folder, got %d", cpSiloToDrive.StatusCode)
	}
	cpDriveToLocal := doReq(http.MethodPost, "/api/vault/copy", strings.NewReader(`{"srcStorage":"gdrive","srcKey":"归档/pic1.jpg","dstStorage":"local"}`), "application/json", true, nil)
	if cpDriveToLocal.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 copying Drive -> Local pool, got %d", cpDriveToLocal.StatusCode)
	}

	// Moving protected blog photo without force=true must return 403
	mvProtected := doReq(http.MethodPost, "/api/vault/copy", strings.NewReader(`{"srcStorage":"silo","srcKey":"photos/pic1.jpg","dstStorage":"silo","dstFolder":"images/memes","move":true}`), "application/json", true, nil)
	if mvProtected.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 moving protected blog asset without force, got %d", mvProtected.StatusCode)
	}
	// Moving into the exact same folder is a safe 200 no-op
	mvNoop := doReq(http.MethodPost, "/api/vault/copy", strings.NewReader(`{"srcStorage":"silo","srcKey":"images/memes/pic1.jpg","dstStorage":"silo","dstFolder":"images/memes","move":true}`), "application/json", true, nil)
	if mvNoop.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 no-op moving file into same folder, got %d", mvNoop.StatusCode)
	}
	// Copy another file into images/memes, then move it to Drive 2026-11 and verify source was removed
	_ = doReq(http.MethodPost, "/api/vault/copy", strings.NewReader(`{"srcStorage":"silo","srcKey":"photos/pic1.jpg","dstStorage":"silo","dstFolder":"images/memes"}`), "application/json", true, nil)
	mvSiloToDrive := doReq(http.MethodPost, "/api/vault/copy", strings.NewReader(`{"srcStorage":"silo","srcKey":"images/memes/pic1-2.jpg","dstStorage":"gdrive","dstFolder":"2026-11","move":true}`), "application/json", true, nil)
	if mvSiloToDrive.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 moving Silo -> Drive folder, got %d", mvSiloToDrive.StatusCode)
	}
	checkGone := doReq(http.MethodGet, "/api/silo/object?key=images/memes/pic1-2.jpg", nil, "", true, nil)
	if checkGone.StatusCode != http.StatusNotFound {
		t.Fatalf("expected moved Silo source images/memes/pic1-2.jpg to be deleted, got %d", checkGone.StatusCode)
	}

	// Deleting protected blog folder `photos` must return 403
	delProtDir := doReq(http.MethodDelete, "/api/silo/folders?folder=photos&force=1", nil, "", true, nil)
	if delProtDir.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 deleting protected blog folder photos, got %d", delProtDir.StatusCode)
	}
	// Deleting non-empty `images/memes` without force=1 must return 409 Conflict
	delMemesNoForce := doReq(http.MethodDelete, "/api/silo/folders?folder=images/memes", nil, "", true, nil)
	if delMemesNoForce.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 deleting non-empty Silo folder without force, got %d", delMemesNoForce.StatusCode)
	}
	// With force=1 it deletes cleanly
	delMemesForce := doReq(http.MethodDelete, "/api/silo/folders?folder=images/memes&force=1", nil, "", true, nil)
	if delMemesForce.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 deleting Silo folder with force=1, got %d", delMemesForce.StatusCode)
	}

	// Deleting non-empty Drive folder `归档` without force=1 returns 409; with force=1 returns 200
	delDriveNoForce := doReq(http.MethodDelete, "/api/gdrive/folders?folder="+url.QueryEscape("归档"), nil, "", true, nil)
	if delDriveNoForce.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 deleting non-empty Drive folder without force, got %d", delDriveNoForce.StatusCode)
	}
	delDriveForce := doReq(http.MethodDelete, "/api/gdrive/folders?folder="+url.QueryEscape("归档")+"&force=1", nil, "", true, nil)
	if delDriveForce.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 deleting Drive folder with force=1, got %d", delDriveForce.StatusCode)
	}
}
