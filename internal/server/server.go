package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"oarc-drop/internal/mediameta"
	"oarc-drop/internal/s3client"
	"oarc-drop/internal/store"
)

const DefaultChunkSize int64 = 20 << 20 // 20 MiB (< Cloudflare 100MB limit)

type HeaderLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type Config struct {
	DataDir           string
	S3                *s3client.Client
	SiloDropPrefix    string // default "aurora/drop/"
	GDriveRemoteDir   string // e.g. "gdrive:OArc-Drop" (empty disables Google Drive target)
	RcloneBin         string // default "rclone"
	RcloneConfig      string // optional path to rclone.conf
	RequireCFAccess   bool   // require Cf-Access-Authenticated-User-Email orCf-Access-Jwt-Assertion on /api/*
	AdminToken        string // optional fallback bearer token for CLI / local use
	PublicBaseURL     string // optional e.g. https://drop.arcueidblog.top
	HeaderLinks       []HeaderLink
	StaticFS          fs.FS
}

type MediaItem struct {
	Key          string    `json:"key"`
	Name         string    `json:"name"`
	Folder       string    `json:"folder"`
	Kind         string    `json:"kind"` // "image" | "audio"
	Ext          string    `json:"ext"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"lastModified"`
	PublicURL    string    `json:"publicUrl,omitempty"`
	StreamURL    string    `json:"streamUrl"`
	ThumbURL     string    `json:"thumbUrl,omitempty"`
	LyricsKey    string    `json:"lyricsKey,omitempty"`
}

type cachedTrackMeta struct {
	Meta   mediameta.TrackMetadata `json:"meta"`
	Lyrics string                  `json:"lyrics,omitempty"`
}

type Server struct {
	cfg   Config
	store *store.Store
	mux   *http.ServeMux

	sseMu      sync.Mutex
	sseClients map[chan string]struct{}

	mediaMu      sync.RWMutex
	mediaFetched time.Time
	mediaItems   []MediaItem
	lrcMap       map[string]string // audio key -> lrc key

	trackMu    sync.RWMutex
	trackCache map[string]cachedTrackMeta
}

func New(cfg Config, st *store.Store) *Server {
	if cfg.SiloDropPrefix == "" {
		cfg.SiloDropPrefix = "aurora/drop/"
	}
	if !strings.HasSuffix(cfg.SiloDropPrefix, "/") {
		cfg.SiloDropPrefix += "/"
	}
	if cfg.RcloneBin == "" {
		cfg.RcloneBin = "rclone"
	}
	s := &Server{
		cfg:        cfg,
		store:      st,
		mux:        http.NewServeMux(),
		sseClients: make(map[chan string]struct{}),
		lrcMap:     make(map[string]string),
		trackCache: make(map[string]cachedTrackMeta),
	}
	s.registerRoutes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// Workspace API (protected by Cloudflare Access / AdminToken when enabled)
	s.mux.HandleFunc("GET /api/status", s.adminOnly(s.handleStatus))
	s.mux.HandleFunc("GET /api/events", s.adminOnly(s.handleSSE))

	// Clipboard
	s.mux.HandleFunc("GET /api/clips", s.adminOnly(s.handleListClips))
	s.mux.HandleFunc("POST /api/clips/text", s.adminOnly(s.handleCreateTextClip))
	s.mux.HandleFunc("POST /api/clips/file", s.adminOnly(s.handleCreateFileClip))
	s.mux.HandleFunc("GET /api/clips/{id}/raw", s.adminOnly(s.handleGetClipRaw))
	s.mux.HandleFunc("PATCH /api/clips/{id}", s.adminOnly(s.handlePatchClip))
	s.mux.HandleFunc("POST /api/clips/{id}/share", s.adminOnly(s.handleShareClip))
	s.mux.HandleFunc("DELETE /api/clips/{id}", s.adminOnly(s.handleDeleteClip))

	// Chunked Uploads (20 MiB per request)
	s.mux.HandleFunc("POST /api/uploads/init", s.adminOnly(s.handleUploadInit))
	s.mux.HandleFunc("PUT /api/uploads/{id}/part/{part}", s.adminOnly(s.handleUploadPart))
	s.mux.HandleFunc("POST /api/uploads/{id}/complete", s.adminOnly(s.handleUploadComplete))
	s.mux.HandleFunc("DELETE /api/uploads/{id}", s.adminOnly(s.handleUploadAbort))

	// Shares management
	s.mux.HandleFunc("GET /api/shares", s.adminOnly(s.handleListShares))
	s.mux.HandleFunc("POST /api/shares/from-silo", s.adminOnly(s.handleCreateShareFromSilo))
	s.mux.HandleFunc("DELETE /api/shares/{code}", s.adminOnly(s.handleDeleteShare))

	// Silo Media Index (Gallery + Music)
	s.mux.HandleFunc("GET /api/media", s.adminOnly(s.handleListMedia))
	s.mux.HandleFunc("GET /api/media/stream", s.adminOnly(s.handleStreamMedia))
	s.mux.HandleFunc("GET /api/media/track-meta", s.adminOnly(s.handleTrackMeta))
	s.mux.HandleFunc("GET /api/media/cover", s.adminOnly(s.handleTrackCover))

	// Public share endpoints — ALL under /s/* so a single Cloudflare Access Bypass rule (/s/*) covers UI + meta + download!
	s.mux.HandleFunc("GET /s/{code}/meta", s.handlePublicShareMeta)
	s.mux.HandleFunc("POST /s/{code}/verify", s.handlePublicShareVerify)
	s.mux.HandleFunc("GET /s/{code}/raw", s.handlePublicShareRaw)

	// Static SPA fallback (also serves /s/{code} HTML shell!)
	s.mux.HandleFunc("/", s.handleSPA)
}

func (s *Server) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.isAuthorizedAdmin(r) {
			next(w, r)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
}

func (s *Server) isAuthorizedAdmin(r *http.Request) bool {
	if s.cfg.AdminToken != "" {
		auth := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if auth != "" && subtle.ConstantTimeCompare([]byte(auth), []byte(s.cfg.AdminToken)) == 1 {
			return true
		}
		if c, err := r.Cookie("oarc_admin"); err == nil {
			if subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.cfg.AdminToken)) == 1 {
				return true
			}
		}
	}
	if !s.cfg.RequireCFAccess {
		return s.cfg.AdminToken == ""
	}
	// Cloudflare Access injects Cf-Access-Jwt-Assertion & Cf-Access-Authenticated-User-Email
	// (Nginx sits behind Cloudflare IP allowlist in gateway/nginx.conf).
	return r.Header.Get("Cf-Access-Jwt-Assertion") != "" || r.Header.Get("Cf-Access-Authenticated-User-Email") != ""
}

// --- Status & SSE ---

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	var diskTotal, diskFree uint64
	var stat syscall.Statfs_t
	if syscall.Statfs(s.cfg.DataDir, &stat) == nil {
		diskTotal = stat.Blocks * uint64(stat.Bsize)
		diskFree = stat.Bavail * uint64(stat.Bsize)
	}
	gdriveReady := false
	if s.cfg.GDriveRemoteDir != "" {
		if _, err := exec.LookPath(s.cfg.RcloneBin); err == nil {
			gdriveReady = true
		}
	}
	userEmail := r.Header.Get("Cf-Access-Authenticated-User-Email")
	writeJSON(w, http.StatusOK, map[string]any{
		"chunkSize":     DefaultChunkSize,
		"diskTotal":     diskTotal,
		"diskFree":      diskFree,
		"siloEnabled":   s.cfg.S3 != nil,
		"siloBucket":    s.siloBucketName(),
		"siloPrefix":    s.cfg.SiloDropPrefix,
		"gdriveEnabled": gdriveReady,
		"gdriveTarget":  s.cfg.GDriveRemoteDir,
		"userEmail":     userEmail,
		"headerLinks":   s.cfg.HeaderLinks,
	})
}

func (s *Server) siloBucketName() string {
	if s.cfg.S3 == nil {
		return ""
	}
	return s.cfg.S3.Bucket()
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan string, 16)
	s.sseMu.Lock()
	s.sseClients[ch] = struct{}{}
	s.sseMu.Unlock()
	defer func() {
		s.sseMu.Lock()
		delete(s.sseClients, ch)
		s.sseMu.Unlock()
	}()

	fmt.Fprintf(w, "event: connected\ndata: {\"ts\":%d}\n\n", time.Now().Unix())
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": ping %d\n\n", time.Now().Unix())
			flusher.Flush()
		}
	}
}

func (s *Server) broadcast(event string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(raw))
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	for ch := range s.sseClients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// --- Clipboard ---

func (s *Server) handleListClips(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.store.ListClips()})
}

func (s *Server) handleCreateTextClip(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text is empty"})
		return
	}
	item, err := s.store.AddTextClip(body.Text)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcast("clip.created", item)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleCreateFileClip(w http.ResponseWriter, r *http.Request) {
	// Direct lossless clipboard paste for screenshots / files up to 25 MiB
	r.Body = http.MaxBytesReader(w, r.Body, 25<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file exceeds quick-paste limit, use chunked drop"})
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file field"})
		return
	}
	defer file.Close()

	id := fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	safeName := store.SanitizeFileName(hdr.Filename)
	rel := filepath.Join("clips", id+"_"+safeName)
	dstPath := filepath.Join(s.cfg.DataDir, rel)

	out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot write clip file"})
		return
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, hasher), file)
	_ = out.Close()
	if copyErr != nil {
		_ = os.Remove(dstPath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "write failed"})
		return
	}

	mimeType := hdr.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		if guessed := mime.TypeByExtension(strings.ToLower(filepath.Ext(safeName))); guessed != "" {
			mimeType = guessed
		} else {
			mimeType = "application/octet-stream"
		}
	}
	kind := "file"
	if strings.HasPrefix(mimeType, "image/") {
		kind = "image"
	}
	shaHex := hex.EncodeToString(hasher.Sum(nil))
	item, err := s.store.AddFileClip("", kind, safeName, mimeType, written, shaHex[:16], rel, "")
	if err != nil {
		_ = os.Remove(dstPath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcast("clip.created", item)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleGetClipRaw(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.store.GetClip(id)
	if err != nil || c.LocalRel == "" {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(s.cfg.DataDir, c.LocalRel)
	f, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if c.MimeType != "" {
		w.Header().Set("Content-Type", c.MimeType)
	}
	setContentDisposition(w, c.FileName, r.URL.Query().Get("download") == "1")
	http.ServeContent(w, r, c.FileName, st.ModTime(), f)
}

func (s *Server) handlePatchClip(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Pinned bool `json:"pinned"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	item, err := s.store.SetClipPinned(id, body.Pinned)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "clip not found"})
		return
	}
	s.broadcast("clip.updated", item)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleShareClip(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.store.GetClip(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "clip not found"})
		return
	}
	var opts struct {
		TTLHours     int    `json:"ttlHours"`
		MaxDownloads int    `json:"maxDownloads"`
		Passcode     string `json:"passcode"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&opts)

	var storageKey, fileName, mimeType string
	var size int64
	if c.Kind == "text" {
		// Materialize text clip as a lossless UTF-8 .txt file for public download/view
		fileName = "clip-" + c.ID[:6] + ".txt"
		mimeType = "text/plain; charset=utf-8"
		rel := filepath.Join("files", "clip_"+c.ID+".txt")
		if err := os.WriteFile(filepath.Join(s.cfg.DataDir, rel), []byte(c.Text), 0o644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to materialize text"})
			return
		}
		storageKey = rel
		size = int64(len(c.Text))
	} else {
		storageKey = c.LocalRel
		fileName = c.FileName
		mimeType = c.MimeType
		size = c.Size
	}

	var exp *time.Time
	if opts.TTLHours > 0 {
		t := time.Now().UTC().Add(time.Duration(opts.TTLHours) * time.Hour)
		exp = &t
	}
	sh, err := s.store.CreateShare(store.ShareItem{
		FileName:     fileName,
		MimeType:     mimeType,
		Size:         size,
		SHA256:       c.SHA256,
		Storage:      "local",
		StorageKey:   storageKey,
		ExpiresAt:    exp,
		MaxDownloads: opts.MaxDownloads,
	}, opts.Passcode)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	updated, _ := s.store.AttachShareToClip(id, sh.Code)
	if updated != nil {
		s.broadcast("clip.updated", updated)
	}
	s.broadcast("share.created", sh)
	writeJSON(w, http.StatusCreated, sh)
}

func (s *Server) handleDeleteClip(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteClip(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	s.broadcast("clip.deleted", map[string]string{"id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- 20 MiB Chunked Upload Engine (Local / Silo / Google Drive) ---

func (s *Server) handleUploadInit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FileName     string `json:"fileName"`
		Size         int64  `json:"size"`
		MimeType     string `json:"mimeType"`
		Target       string `json:"target"` // "local" | "silo" | "gdrive"
		SiloPrefix   string `json:"siloPrefix"`
		TTLHours     int    `json:"ttlHours"`
		MaxDownloads int    `json:"maxDownloads"`
		Passcode     string `json:"passcode"`
		AddToClip    bool   `json:"addToClip"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if req.Size < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid size"})
		return
	}
	switch req.Target {
	case "local", "":
		req.Target = "local"
	case "silo":
		if s.cfg.S3 == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Silo S3 is not configured"})
			return
		}
	case "gdrive":
		if s.cfg.GDriveRemoteDir == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Google Drive target is not configured"})
			return
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported storage target"})
		return
	}

	safeName := store.SanitizeFileName(req.FileName)
	mimeType := strings.TrimSpace(req.MimeType)
	if mimeType == "" || mimeType == "application/octet-stream" {
		if g := mime.TypeByExtension(strings.ToLower(filepath.Ext(safeName))); g != "" {
			mimeType = g
		} else {
			mimeType = "application/octet-stream"
		}
	}

	totalParts := 1
	if req.Size > 0 {
		totalParts = int((req.Size + DefaultChunkSize - 1) / DefaultChunkSize)
	}
	u := &store.UploadSession{
		FileName:     safeName,
		MimeType:     mimeType,
		Size:         req.Size,
		ChunkSize:    DefaultChunkSize,
		TotalParts:   totalParts,
		Target:       req.Target,
		SiloPrefix:   sanitizeSiloPrefix(req.SiloPrefix, s.cfg.SiloDropPrefix),
		TTLHours:     req.TTLHours,
		MaxDownloads: req.MaxDownloads,
		Passcode:     req.Passcode,
		AddToClip:    req.AddToClip,
	}
	s.store.CreateUploadSession(u)
	if err := os.MkdirAll(filepath.Join(s.cfg.DataDir, "staging", u.ID), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "staging mkdir failed"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"uploadId":   u.ID,
		"chunkSize":  u.ChunkSize,
		"totalParts": u.TotalParts,
	})
}

func (s *Server) handleUploadPart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	partNum, err := strconv.Atoi(r.PathValue("part"))
	if err != nil || partNum < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid part number"})
		return
	}
	u, ok := s.store.GetUploadSession(id)
	if !ok || partNum > u.TotalParts {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "upload session not found"})
		return
	}

	// Enforce per-chunk ceiling (chunkSize + 1 MiB margin)
	r.Body = http.MaxBytesReader(w, r.Body, DefaultChunkSize+(1<<20))
	partPath := filepath.Join(s.cfg.DataDir, "staging", id, fmt.Sprintf("part.%06d", partNum))
	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "open part failed"})
		return
	}
	n, copyErr := io.Copy(f, r.Body)
	_ = f.Close()
	if copyErr != nil {
		_ = os.Remove(partPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "write part failed"})
		return
	}
	_ = s.store.MarkUploadPart(id, partNum, n)
	writeJSON(w, http.StatusOK, map[string]any{"part": partNum, "received": n})
}

func (s *Server) handleUploadComplete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u, ok := s.store.PopUploadSession(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "upload session not found"})
		return
	}
	stagingDir := filepath.Join(s.cfg.DataDir, "staging", id)
	defer os.RemoveAll(stagingDir)

	assembled := filepath.Join(stagingDir, "assembled.bin")
	out, err := os.OpenFile(assembled, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "assemble open failed"})
		return
	}
	hasher := sha256.New()
	var totalWritten int64
	for p := 1; p <= u.TotalParts; p++ {
		partPath := filepath.Join(stagingDir, fmt.Sprintf("part.%06d", p))
		pf, err := os.Open(partPath)
		if err != nil {
			out.Close()
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("missing part %d", p)})
			return
		}
		n, copyErr := io.Copy(io.MultiWriter(out, hasher), pf)
		pf.Close()
		_ = os.Remove(partPath) // free disk immediately as we stitch!
		if copyErr != nil {
			out.Close()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stitch failed"})
			return
		}
		totalWritten += n
	}
	if err := out.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "close assembled failed"})
		return
	}
	if u.Size > 0 && totalWritten != u.Size {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("size mismatch: got %d expected %d", totalWritten, u.Size)})
		return
	}
	shaFull := hex.EncodeToString(hasher.Sum(nil))
	ym := time.Now().UTC().Format("2006-01")

	var storageKey string
	var publicURL string

	switch u.Target {
	case "local":
		rel := filepath.Join("files", id+"_"+u.FileName)
		finalPath := filepath.Join(s.cfg.DataDir, rel)
		if err := os.Rename(assembled, finalPath); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "move local file failed"})
			return
		}
		storageKey = rel

	case "silo":
		objKey := u.SiloPrefix + ym + "/" + shaFull[:12] + "_" + u.FileName
		f, err := os.Open(assembled)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "reopen assembled failed"})
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		putErr := s.cfg.S3.PutObject(ctx, objKey, f, totalWritten, u.MimeType, "public, max-age=2592000, immutable")
		cancel()
		f.Close()
		if putErr != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upload to Silo failed: " + putErr.Error()})
			return
		}
		storageKey = objKey
		publicURL = s.cfg.S3.PublicObjectURL(objKey)
		s.invalidateMediaCache()
		s.broadcast("media.refresh", map[string]string{"key": objKey})

	case "gdrive":
		relKey := ym + "/" + id[:8] + "_" + u.FileName
		dest := strings.TrimRight(s.cfg.GDriveRemoteDir, "/") + "/" + relKey
		args := s.rcloneArgs("copyto", assembled, dest)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		cmd := exec.CommandContext(ctx, s.cfg.RcloneBin, args...)
		outBytes, runErr := cmd.CombinedOutput()
		cancel()
		if runErr != nil {
			log.Printf("[oarc-drop] rclone copyto failed: %v (%s)", runErr, strings.TrimSpace(string(outBytes)))
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upload to Google Drive failed"})
			return
		}
		storageKey = relKey
	}

	var exp *time.Time
	if u.TTLHours > 0 {
		t := time.Now().UTC().Add(time.Duration(u.TTLHours) * time.Hour)
		exp = &t
	}
	sh, err := s.store.CreateShare(store.ShareItem{
		FileName:     u.FileName,
		MimeType:     u.MimeType,
		Size:         totalWritten,
		SHA256:       shaFull[:16],
		Storage:      u.Target,
		StorageKey:   storageKey,
		ExpiresAt:    exp,
		MaxDownloads: u.MaxDownloads,
	}, u.Passcode)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcast("share.created", sh)

	var clipItem *store.ClipItem
	if u.AddToClip {
		kind := "file"
		if strings.HasPrefix(u.MimeType, "image/") {
			kind = "image"
		}
		localRel := ""
		if u.Target == "local" {
			localRel = storageKey
		}
		clipItem, _ = s.store.AddFileClip("", kind, u.FileName, u.MimeType, totalWritten, shaFull[:16], localRel, sh.Code)
		if clipItem != nil {
			s.broadcast("clip.created", clipItem)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"share":     sh,
		"publicUrl": publicURL,
		"clip":      clipItem,
	})
}

func (s *Server) handleUploadAbort(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.store.PopUploadSession(id)
	_ = os.RemoveAll(filepath.Join(s.cfg.DataDir, "staging", id))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- Shares ---

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.store.ListShares()})
}

func (s *Server) handleCreateShareFromSilo(w http.ResponseWriter, r *http.Request) {
	if s.cfg.S3 == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Silo not configured"})
		return
	}
	var req struct {
		Key          string `json:"key"`
		Size         int64  `json:"size"`
		TTLHours     int    `json:"ttlHours"`
		MaxDownloads int    `json:"maxDownloads"`
		Passcode     string `json:"passcode"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req); err != nil || strings.TrimSpace(req.Key) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	cleanKey := strings.TrimLeft(req.Key, "/")
	name := path.Base(cleanKey)
	mimeType := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	var exp *time.Time
	if req.TTLHours > 0 {
		t := time.Now().UTC().Add(time.Duration(req.TTLHours) * time.Hour)
		exp = &t
	}
	sh, err := s.store.CreateShare(store.ShareItem{
		FileName:     name,
		MimeType:     mimeType,
		Size:         req.Size,
		Storage:      "silo",
		StorageKey:   cleanKey,
		ExpiresAt:    exp,
		MaxDownloads: req.MaxDownloads,
	}, req.Passcode)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcast("share.created", sh)
	writeJSON(w, http.StatusCreated, sh)
}

func (s *Server) handleDeleteShare(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	_, _, err := s.store.DeleteShare(code)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	s.broadcast("share.deleted", map[string]string{"code": code})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- Silo Media Indexer (Gallery + Music) ---

func (s *Server) invalidateMediaCache() {
	s.mediaMu.Lock()
	s.mediaFetched = time.Time{}
	s.mediaMu.Unlock()
}

func (s *Server) ensureMediaIndex(ctx context.Context, force bool) ([]MediaItem, error) {
	if s.cfg.S3 == nil {
		return []MediaItem{}, nil
	}
	s.mediaMu.RLock()
	if !force && !s.mediaFetched.IsZero() && time.Since(s.mediaFetched) < 45*time.Second {
		cp := append([]MediaItem(nil), s.mediaItems...)
		s.mediaMu.RUnlock()
		return cp, nil
	}
	s.mediaMu.RUnlock()

	objs, err := s.cfg.S3.ListAll(ctx, "")
	if err != nil {
		return nil, err
	}

	thumbSet := make(map[string]struct{})
	lrcByStem := make(map[string]string)
	for _, o := range objs {
		if strings.Contains(o.Key, "/thumbs/") || strings.HasPrefix(o.Key, "thumbs/") {
			thumbSet[o.Key] = struct{}{}
			continue
		}
		if strings.EqualFold(path.Ext(o.Key), ".lrc") {
			stem := strings.TrimSuffix(o.Key, path.Ext(o.Key))
			lrcByStem[stem] = o.Key
		}
	}

	items := make([]MediaItem, 0, len(objs))
	newLrcMap := make(map[string]string)
	for _, o := range objs {
		if o.Size == 0 || strings.HasSuffix(o.Key, "/") {
			continue
		}
		// Skip Aurora generated thumbnail duplicates from appearing as standalone photos
		if strings.Contains(o.Key, "/thumbs/") || strings.HasPrefix(o.Key, "thumbs/") {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(path.Ext(o.Key), "."))
		kind := classifyMediaExt(ext)
		if kind == "" {
			continue
		}
		dir := path.Dir(o.Key)
		if dir == "." {
			dir = "/"
		}
		name := path.Base(o.Key)
		item := MediaItem{
			Key:          o.Key,
			Name:         name,
			Folder:       dir,
			Kind:         kind,
			Ext:          strings.ToUpper(ext),
			Size:         o.Size,
			LastModified: o.LastModified,
			PublicURL:    s.cfg.S3.PublicObjectURL(o.Key),
			StreamURL:    "/api/media/stream?key=" + url.QueryEscape(o.Key),
		}
		if kind == "image" {
			candThumb := path.Join(path.Dir(o.Key), "thumbs", name)
			if _, exists := thumbSet[candThumb]; exists {
				item.ThumbURL = "/api/media/stream?key=" + url.QueryEscape(candThumb)
			} else {
				item.ThumbURL = item.StreamURL
			}
		} else if kind == "audio" {
			stem := strings.TrimSuffix(o.Key, path.Ext(o.Key))
			if lk, ok := lrcByStem[stem]; ok {
				item.LyricsKey = lk
				newLrcMap[o.Key] = lk
			}
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].LastModified.After(items[j].LastModified)
	})

	s.mediaMu.Lock()
	s.mediaItems = items
	s.lrcMap = newLrcMap
	s.mediaFetched = time.Now()
	s.mediaMu.Unlock()
	return items, nil
}

func classifyMediaExt(ext string) string {
	switch ext {
	case "jpg", "jpeg", "png", "webp", "gif", "avif", "svg", "bmp", "ico":
		return "image"
	case "mp3", "flac", "wav", "m4a", "aac", "ogg", "opus":
		return "audio"
	default:
		return ""
	}
}

func (s *Server) handleListMedia(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	force := r.URL.Query().Get("refresh") == "1"
	items, err := s.ensureMediaIndex(ctx, force)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	kindFilter := r.URL.Query().Get("kind")
	folderFilter := r.URL.Query().Get("folder")
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	foldersSet := make(map[string]int)
	filtered := make([]MediaItem, 0, len(items))
	var imgCount, audioCount int
	for _, it := range items {
		if it.Kind == "image" {
			imgCount++
		} else if it.Kind == "audio" {
			audioCount++
		}
		if kindFilter != "" && kindFilter != "all" && it.Kind != kindFilter {
			continue
		}
		foldersSet[it.Folder]++
		if folderFilter != "" && it.Folder != folderFilter {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(it.Key), q) {
			continue
		}
		filtered = append(filtered, it)
	}

	folders := make([]string, 0, len(foldersSet))
	for f := range foldersSet {
		folders = append(folders, f)
	}
	sort.Strings(folders)

	writeJSON(w, http.StatusOK, map[string]any{
		"items":      filtered,
		"folders":    folders,
		"imageCount": imgCount,
		"audioCount": audioCount,
	})
}

func (s *Server) handleStreamMedia(w http.ResponseWriter, r *http.Request) {
	if s.cfg.S3 == nil {
		http.NotFound(w, r)
		return
	}
	key := strings.TrimLeft(r.URL.Query().Get("key"), "/")
	if key == "" {
		http.NotFound(w, r)
		return
	}
	s.proxyS3Object(w, r, key, path.Base(key), r.URL.Query().Get("download") == "1")
}

func (s *Server) handleTrackMeta(w http.ResponseWriter, r *http.Request) {
	if s.cfg.S3 == nil {
		http.NotFound(w, r)
		return
	}
	key := strings.TrimLeft(r.URL.Query().Get("key"), "/")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing key"})
		return
	}
	s.trackMu.RLock()
	if cached, ok := s.trackCache[key]; ok {
		s.trackMu.RUnlock()
		coverURL := ""
		if cached.Meta.HasCover {
			coverURL = "/api/media/cover?key=" + url.QueryEscape(key)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"meta":     cached.Meta,
			"coverUrl": coverURL,
			"lyrics":   cached.Lyrics,
		})
		return
	}
	s.trackMu.RUnlock()

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	// Fetch first 512 KiB to read ID3v2 / FLAC metadata + embedded cover art
	resp, err := s.cfg.S3.GetObject(ctx, key, "bytes=0-524287", "")
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	head, _ := io.ReadAll(io.LimitReader(resp.Body, 524288))
	resp.Body.Close()

	meta := mediameta.ParseAudioHeader(key, head)
	if meta.HasCover && len(meta.CoverBytes) > 0 {
		sum := sha256.Sum256([]byte(key))
		cacheBase := filepath.Join(s.cfg.DataDir, "cache", hex.EncodeToString(sum[:12]))
		_ = os.WriteFile(cacheBase+".img", meta.CoverBytes, 0o644)
		_ = os.WriteFile(cacheBase+".mime", []byte(meta.CoverMime), 0o644)
		meta.CoverBytes = nil // don't retain image bytes in RAM!
	}

	lyrics := ""
	s.mediaMu.RLock()
	lrcKey := s.lrcMap[key]
	s.mediaMu.RUnlock()
	if lrcKey != "" {
		if lResp, lErr := s.cfg.S3.GetObject(ctx, lrcKey, "", ""); lErr == nil {
			b, _ := io.ReadAll(io.LimitReader(lResp.Body, 256<<10))
			lResp.Body.Close()
			lyrics = string(b)
		}
	}

	entry := cachedTrackMeta{Meta: meta, Lyrics: lyrics}
	s.trackMu.Lock()
	s.trackCache[key] = entry
	s.trackMu.Unlock()

	coverURL := ""
	if meta.HasCover {
		coverURL = "/api/media/cover?key=" + url.QueryEscape(key)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"meta":     meta,
		"coverUrl": coverURL,
		"lyrics":   lyrics,
	})
}

func (s *Server) handleTrackCover(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimLeft(r.URL.Query().Get("key"), "/")
	if key == "" {
		http.NotFound(w, r)
		return
	}
	sum := sha256.Sum256([]byte(key))
	cacheBase := filepath.Join(s.cfg.DataDir, "cache", hex.EncodeToString(sum[:12]))
	imgBytes, err := os.ReadFile(cacheBase + ".img")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	mimeBytes, _ := os.ReadFile(cacheBase + ".mime")
	ct := strings.TrimSpace(string(mimeBytes))
	if ct == "" {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(imgBytes)
}

// --- Public Share Portal (/s/{code}/*) ---

func (s *Server) handlePublicShareMeta(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	sh, err := s.store.GetShare(code)
	if err != nil {
		status := http.StatusNotFound
		msg := "分享不存在"
		if errors.Is(err, store.ErrExpired) {
			status = http.StatusGone
			msg = "分享已过期或达到下载上限"
		}
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	unlocked := !sh.HasPasscode || s.hasValidShareCookie(r, sh.Code)
	resp := map[string]any{
		"code":        sh.Code,
		"hasPasscode": sh.HasPasscode,
		"unlocked":    unlocked,
		"createdAt":   sh.CreatedAt,
		"expiresAt":   sh.ExpiresAt,
	}
	if unlocked {
		resp["fileName"] = sh.FileName
		resp["mimeType"] = sh.MimeType
		resp["size"] = sh.Size
		resp["sha256"] = sh.SHA256
		resp["storage"] = sh.Storage
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePublicShareVerify(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	var body struct {
		Passcode string `json:"passcode"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if !s.store.VerifySharePasscode(code, body.Passcode) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "提取码不正确"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oarc_share_" + code,
		Value:    s.signShareToken(code),
		Path:     "/s/" + code,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePublicShareRaw(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	sh, err := s.store.GetShare(code)
	if err != nil {
		http.Error(w, "Share unavailable", http.StatusGone)
		return
	}
	if sh.HasPasscode && !s.hasValidShareCookie(r, code) {
		if qPass := r.URL.Query().Get("passcode"); qPass == "" || !s.store.VerifySharePasscode(code, qPass) {
			http.Error(w, "Passcode required", http.StatusForbidden)
			return
		}
	}

	isDownload := r.URL.Query().Get("download") == "1"
	// Count once per explicit download or initial byte-0 fetch (don't over-count audio range seeks)
	rangeHdr := r.Header.Get("Range")
	if isDownload || rangeHdr == "" || strings.HasPrefix(rangeHdr, "bytes=0-") {
		if err := s.store.IncrementShareDownload(code); err != nil {
			http.Error(w, "Download limit reached", http.StatusGone)
			return
		}
	}

	switch sh.Storage {
	case "local":
		full := filepath.Join(s.cfg.DataDir, sh.StorageKey)
		f, err := os.Open(full)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if sh.MimeType != "" {
			w.Header().Set("Content-Type", sh.MimeType)
		}
		setContentDisposition(w, sh.FileName, isDownload)
		http.ServeContent(w, r, sh.FileName, st.ModTime(), f)

	case "silo":
		if s.cfg.S3 == nil {
			http.Error(w, "Storage offline", http.StatusBadGateway)
			return
		}
		s.proxyS3Object(w, r, sh.StorageKey, sh.FileName, isDownload)

	case "gdrive":
		if s.cfg.GDriveRemoteDir == "" || strings.Contains(sh.StorageKey, "..") {
			http.Error(w, "Google Drive unavailable", http.StatusBadGateway)
			return
		}
		remotePath := strings.TrimRight(s.cfg.GDriveRemoteDir, "/") + "/" + strings.TrimLeft(sh.StorageKey, "/")
		args := s.rcloneArgs("cat", remotePath)
		cmd := exec.CommandContext(r.Context(), s.cfg.RcloneBin, args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil || cmd.Start() != nil {
			http.Error(w, "Google Drive stream error", http.StatusBadGateway)
			return
		}
		if sh.MimeType != "" {
			w.Header().Set("Content-Type", sh.MimeType)
		}
		if sh.Size > 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(sh.Size, 10))
		}
		setContentDisposition(w, sh.FileName, isDownload)
		_, _ = io.Copy(w, stdout)
		_ = cmd.Wait()
	}
}

func (s *Server) proxyS3Object(w http.ResponseWriter, r *http.Request, key string, fileName string, forceDownload bool) {
	resp, err := s.cfg.S3.GetObject(r.Context(), key, r.Header.Get("Range"), r.Header.Get("If-None-Match"))
	if err != nil {
		http.Error(w, "Object not found", http.StatusNotFound)
		return
	}
	defer resp.Body.Close()

	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		if ct := mime.TypeByExtension(strings.ToLower(path.Ext(fileName))); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	setContentDisposition(w, fileName, forceDownload)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) signShareToken(code string) string {
	mac := hmac.New(sha256.New, s.store.SecretSeed())
	mac.Write([]byte("share:" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) hasValidShareCookie(r *http.Request, code string) bool {
	c, err := r.Cookie("oarc_share_" + code)
	if err != nil {
		return false
	}
	expected := s.signShareToken(code)
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(expected)) == 1
}

func (s *Server) rcloneArgs(subcmd string, rest ...string) []string {
	var args []string
	if s.cfg.RcloneConfig != "" {
		args = append(args, "--config", s.cfg.RcloneConfig)
	}
	args = append(args, subcmd)
	args = append(args, rest...)
	return args
}

// --- Static SPA ---

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if s.cfg.StaticFS == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("oarc-drop API server running"))
		return
	}
	cleanPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if cleanPath != "" && !strings.HasPrefix(r.URL.Path, "/s/") {
		if f, err := s.cfg.StaticFS.Open(cleanPath); err == nil {
			f.Close()
			if strings.HasPrefix(cleanPath, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.FileServer(http.FS(s.cfg.StaticFS)).ServeHTTP(w, r)
			return
		}
	}
	// Serve index.html for "/" and "/s/{code}" SPA routes
	idx, err := fs.ReadFile(s.cfg.StaticFS, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(idx)
}

func sanitizeSiloPrefix(input string, fallback string) string {
	p := strings.TrimSpace(input)
	if p == "" {
		return fallback
	}
	p = strings.Trim(p, "/")
	if p == "" || strings.Contains(p, "..") {
		return fallback
	}
	return p + "/"
}

func setContentDisposition(w http.ResponseWriter, fileName string, attachment bool) {
	disp := "inline"
	if attachment {
		disp = "attachment"
	}
	escaped := url.PathEscape(fileName)
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename*=UTF-8''%s", disp, escaped))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
