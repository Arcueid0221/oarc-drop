package main

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"oarc-drop/internal/s3client"
	"oarc-drop/internal/server"
	"oarc-drop/internal/store"
)

type memObj struct {
	data  []byte
	ctype string
}

type inProcessTransport struct {
	h http.Handler
}

type roundTripRecorder struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (r *roundTripRecorder) Header() http.Header         { return r.header }
func (r *roundTripRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *roundTripRecorder) WriteHeader(statusCode int)  { r.code = statusCode }

func (t inProcessTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := &roundTripRecorder{header: make(http.Header), code: http.StatusOK}
	t.h.ServeHTTP(rec, req)
	return &http.Response{
		StatusCode:    rec.code,
		Header:        rec.header,
		Body:          io.NopCloser(bytes.NewReader(rec.body.Bytes())),
		ContentLength: int64(rec.body.Len()),
		Request:       req,
	}, nil
}

func main() {
	addr := "127.0.0.1:8095"
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		addr = v
	}
	tmpRoot := filepath.Join(os.TempDir(), "oarc-drop-demo")
	_ = os.MkdirAll(filepath.Join(tmpRoot, "gdrive"), 0o755)

	fakeRclone := filepath.Join(tmpRoot, "rclone-local-sim.sh")
	script := fmt.Sprintf(`#!/bin/sh
set -e
if [ "$1" = "copyto" ]; then
  dst=$(echo "$3" | sed 's|^gdrive:OArc-Drop/|%s/gdrive/|')
  mkdir -p "$(dirname "$dst")"
  cp "$2" "$dst"
elif [ "$1" = "cat" ]; then
  src=$(echo "$2" | sed 's|^gdrive:OArc-Drop/|%s/gdrive/|')
  cat "$src"
fi
`, tmpRoot, tmpRoot)
	_ = os.WriteFile(fakeRclone, []byte(script), 0o755)

	svgAurora := `<svg xmlns="http://www.w3.org/2000/svg" width="960" height="640" viewBox="0 0 960 640"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0%" stop-color="#38bdf8"/><stop offset="50%" stop-color="#818cf8"/><stop offset="100%" stop-color="#f472b6"/></linearGradient></defs><rect width="960" height="640" fill="url(#g)"/><circle cx="290" cy="240" r="150" fill="rgba(255,255,255,0.32)"/><circle cx="680" cy="390" r="190" fill="rgba(255,255,255,0.22)"/></svg>`
	svgGlacier := `<svg xmlns="http://www.w3.org/2000/svg" width="960" height="640" viewBox="0 0 960 640"><defs><linearGradient id="g2" x1="0" y1="1" x2="1" y2="0"><stop offset="0%" stop-color="#0ea5e9"/><stop offset="100%" stop-color="#2dd4bf"/></linearGradient></defs><rect width="960" height="640" fill="url(#g2)"/><circle cx="480" cy="320" r="170" fill="rgba(255,255,255,0.36)"/></svg>`

	var mu sync.RWMutex
	objs := map[string]memObj{
		"aurora/photos/liquid-aurora.svg":  {data: []byte(svgAurora), ctype: "image/svg+xml"},
		"aurora/photos/glacier-prism.svg":  {data: []byte(svgGlacier), ctype: "image/svg+xml"},
		"aurora/music/crystal-prelude.wav": {data: synthChordWAV(6), ctype: "audio/wav"},
		"aurora/music/crystal-prelude.lrc": {
			data:  []byte("[00:00.00] 冰晶序曲 · Liquid Glass Demo\n[00:02.00] 切换顶部标签页，音乐不会中断\n[00:04.00] 任意页面按 ⌘V 即可无损粘贴原图或文本\n"),
			ctype: "text/plain; charset=utf-8",
		},
	}

	siloHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/aurora"), "/")
		if r.Method == http.MethodGet && key == "" && r.URL.Query().Get("list-type") == "2" {
			mu.RLock()
			defer mu.RUnlock()
			type item struct {
				Key          string `xml:"Key"`
				LastModified string `xml:"LastModified"`
				ETag         string `xml:"ETag"`
				Size         int64  `xml:"Size"`
			}
			var res struct {
				XMLName  xml.Name `xml:"ListBucketResult"`
				Contents []item   `xml:"Contents"`
			}
			for k, o := range objs {
				res.Contents = append(res.Contents, item{
					Key:          k,
					LastModified: time.Now().UTC().Format(time.RFC3339),
					ETag:         "\"demo\"",
					Size:         int64(len(o.data)),
				})
			}
			w.Header().Set("Content-Type", "application/xml")
			_ = xml.NewEncoder(w).Encode(res)
			return
		}
		if r.Method == http.MethodPut {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			objs[key] = memObj{data: b, ctype: r.Header.Get("Content-Type")}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		mu.RLock()
		o, ok := objs[key]
		mu.RUnlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", o.ctype)
		http.ServeContent(w, r, filepath.Base(key), time.Now(), bytes.NewReader(o.data))
	})

	s3c, _ := s3client.New(s3client.Config{
		Endpoint:  "http://minio:9000",
		AccessKey: "demo",
		SecretKey: "demo",
		Bucket:    "aurora",
		PublicURL: "http://" + addr + "/api/media/stream?key=",
		Transport: inProcessTransport{h: siloHandler},
	})

	st, err := store.New(filepath.Join(tmpRoot, "data"), 100)
	if err != nil {
		log.Fatal(err)
	}
	if len(st.ListClips()) == 0 {
		_, _ = st.AddTextClip("ssh blog  # 示例剪贴板条目：点击下方按钮即可一键复制或生成公开分享短链")
	}

	srv := server.New(server.Config{
		DataDir:         filepath.Join(tmpRoot, "data"),
		S3:              s3c,
		GDriveRemoteDir: "gdrive:OArc-Drop",
		RcloneBin:       fakeRclone,
		RequireCFAccess: false,
		HeaderLinks: []server.HeaderLink{
			{Label: "Blog", URL: "https://arcueidblog.top"},
			{Label: "Monitor", URL: "https://monitor.arcueidblog.top"},
			{Label: "SIM", URL: "https://sim.arcueidblog.top"},
		},
		StaticFS: os.DirFS("./web/dist"),
	}, st)

	log.Printf("[oarc-drop demo] Apple Liquid Glass UI preview ready at http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, srv))
}

func synthChordWAV(seconds int) []byte {
	const sr = 22050
	samples := sr * seconds
	var pcm bytes.Buffer
	freqs := []float64{261.63, 329.63, 392.00, 493.88} // Cmaj7 warm glass chord
	for i := 0; i < samples; i++ {
		t := float64(i) / sr
		env := math.Sin(math.Pi * t / float64(seconds))
		var v float64
		for _, f := range freqs {
			v += math.Sin(2 * math.Pi * f * t)
		}
		s := int16(v / float64(len(freqs)) * env * 9000)
		_ = binary.Write(&pcm, binary.LittleEndian, s)
	}
	dataLen := uint32(pcm.Len())
	var wav bytes.Buffer
	wav.WriteString("RIFF")
	_ = binary.Write(&wav, binary.LittleEndian, uint32(36)+dataLen)
	wav.WriteString("WAVEfmt ")
	_ = binary.Write(&wav, binary.LittleEndian, uint32(16))
	_ = binary.Write(&wav, binary.LittleEndian, uint16(1))
	_ = binary.Write(&wav, binary.LittleEndian, uint16(1))
	_ = binary.Write(&wav, binary.LittleEndian, uint32(sr))
	_ = binary.Write(&wav, binary.LittleEndian, uint32(sr*2))
	_ = binary.Write(&wav, binary.LittleEndian, uint16(2))
	_ = binary.Write(&wav, binary.LittleEndian, uint16(16))
	wav.WriteString("data")
	_ = binary.Write(&wav, binary.LittleEndian, dataLen)
	wav.Write(pcm.Bytes())
	return wav.Bytes()
}
