package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"time"

	"oarc-drop/internal/qrcode"
	"oarc-drop/internal/store"
)

type VaultObject struct {
	Key          string    `json:"key"`
	Name         string    `json:"name"`
	Folder       string    `json:"folder"`
	Kind         string    `json:"kind"` // "image" | "audio" | "video" | "doc" | "archive" | "other"
	Ext          string    `json:"ext"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"lastModified"`
	PublicURL    string    `json:"publicUrl,omitempty"`
	StreamURL    string    `json:"streamUrl"`
	ThumbURL     string    `json:"thumbUrl,omitempty"`
	Protected    bool      `json:"protected"`
}

type GDriveFile struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	Folder       string    `json:"folder"`
	Kind         string    `json:"kind"`
	Ext          string    `json:"ext"`
	Size         int64     `json:"size"`
	MimeType     string    `json:"mimeType"`
	LastModified time.Time `json:"lastModified"`
	StreamURL    string    `json:"streamUrl"`
}

type CatalogPlaylist struct {
	ID         int      `json:"id"`
	Name       string   `json:"name"`
	CoverURL   string   `json:"coverUrl,omitempty"`
	TrackCount int      `json:"trackCount"`
	TrackKeys  []string `json:"trackKeys"`
}

type CatalogTrackInfo struct {
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album,omitempty"`
	CoverURL   string `json:"coverUrl,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
}

type MusicCatalog struct {
	Playlists []CatalogPlaylist           `json:"playlists"`
	Tracks    map[string]CatalogTrackInfo `json:"tracks"`
}

var protectedBlogPrefixes = []string{
	"articles/",
	"photos/",
	"talks/",
	"music/",
	"character/",
	"config/",
	"avatar/",
}

func isProtectedSiloKey(key string) bool {
	clean := strings.TrimLeft(key, "/")
	for _, p := range protectedBlogPrefixes {
		if strings.HasPrefix(clean, p) {
			return true
		}
	}
	return false
}

func classifyAnyExt(ext string) string {
	switch strings.ToLower(ext) {
	case "jpg", "jpeg", "png", "webp", "gif", "avif", "svg", "bmp", "ico":
		return "image"
	case "mp3", "flac", "wav", "m4a", "aac", "ogg", "opus":
		return "audio"
	case "mp4", "webm", "mov", "mkv":
		return "video"
	case "pdf", "md", "txt", "json", "yaml", "yml", "lrc", "csv", "log", "html":
		return "doc"
	case "zip", "tar", "gz", "tgz", "7z", "rar", "dmg", "apk", "ipa":
		return "archive"
	default:
		return "other"
	}
}

// GET /api/qr?text=...
func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	txt := strings.TrimSpace(r.URL.Query().Get("text"))
	if txt == "" {
		http.Error(w, "missing text", http.StatusBadRequest)
		return
	}
	svg, err := qrcode.RenderSVG(txt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(svg)
}

// resolveCleanSiloKey keeps clean human-readable filenames (e.g. images/chart.png or drop/archive.zip)
// and appends -2, -3 only if an object with the exact same key already exists.
func (s *Server) resolveCleanSiloKey(ctx context.Context, prefix string, fileName string) string {
	cleanPrefix := sanitizeSiloPrefix(prefix, s.cfg.SiloDropPrefix)
	safeName := store.SanitizeFileName(fileName)
	lower := strings.ToLower(safeName)
	if lower == "image.png" || lower == "image.jpg" || lower == "image.jpeg" || lower == "image.webp" || strings.HasPrefix(lower, "clipboard") {
		ext := path.Ext(safeName)
		if ext == "" {
			ext = ".png"
		}
		safeName = "paste-" + time.Now().Format("20060102-150405") + ext
	}
	cand := cleanPrefix + safeName
	if s.cfg.S3 == nil {
		return cand
	}
	if existing, err := s.cfg.S3.HeadObject(ctx, cand); err == nil && existing == nil {
		return cand
	}
	ext := path.Ext(safeName)
	stem := strings.TrimSuffix(safeName, ext)
	for i := 2; i <= 200; i++ {
		next := fmt.Sprintf("%s%s-%d%s", cleanPrefix, stem, i, ext)
		if existing, err := s.cfg.S3.HeadObject(ctx, next); err == nil && existing == nil {
			return next
		}
	}
	return fmt.Sprintf("%s%s-%d%s", cleanPrefix, stem, time.Now().Unix()%10000, ext)
}

// GET /api/silo/objects?includeThumbs=0&refresh=0
func (s *Server) handleListSiloObjects(w http.ResponseWriter, r *http.Request) {
	if s.cfg.S3 == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []VaultObject{}, "folders": []string{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	objs, err := s.cfg.S3.ListAll(ctx, "")
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	includeThumbs := r.URL.Query().Get("includeThumbs") == "1"
	thumbSet := make(map[string]struct{})
	coverByBaseStem := make(map[string]string)
	for _, o := range objs {
		if strings.Contains(o.Key, "/thumbs/") || strings.HasPrefix(o.Key, "thumbs/") {
			thumbSet[o.Key] = struct{}{}
		}
		if strings.HasPrefix(o.Key, "music/covers/tracks/") {
			base := path.Base(o.Key)
			stem := strings.TrimSuffix(base, path.Ext(base))
			if stem != "" {
				coverByBaseStem[stem] = o.Key
			}
		}
	}

	items := make([]VaultObject, 0, len(objs))
	folderCounts := make(map[string]int)
	for _, o := range objs {
		if o.Size == 0 && strings.HasSuffix(o.Key, "/") {
			continue
		}
		if !includeThumbs && (strings.Contains(o.Key, "/thumbs/") || strings.HasPrefix(o.Key, "thumbs/")) {
			continue
		}
		dir := path.Dir(o.Key)
		if dir == "." {
			dir = "/"
		}
		name := path.Base(o.Key)
		ext := strings.TrimPrefix(path.Ext(name), ".")
		kind := classifyAnyExt(ext)
		streamURL := "/api/media/stream?key=" + url.QueryEscape(o.Key)
		thumbURL := ""
		if kind == "image" {
			candThumb := path.Join(path.Dir(o.Key), "thumbs", name)
			if _, ok := thumbSet[candThumb]; ok {
				thumbURL = "/api/media/stream?key=" + url.QueryEscape(candThumb)
			} else {
				thumbURL = streamURL
			}
		} else if kind == "audio" {
			baseStem := strings.TrimSuffix(name, path.Ext(name))
			if ck, ok := coverByBaseStem[baseStem]; ok {
				thumbURL = "/api/media/stream?key=" + url.QueryEscape(ck)
			}
		}
		folderCounts[dir]++
		items = append(items, VaultObject{
			Key:          o.Key,
			Name:         name,
			Folder:       dir,
			Kind:         kind,
			Ext:          strings.ToUpper(ext),
			Size:         o.Size,
			LastModified: o.LastModified,
			PublicURL:    s.cfg.S3.PublicObjectURL(o.Key),
			StreamURL:    streamURL,
			ThumbURL:     thumbURL,
			Protected:    isProtectedSiloKey(o.Key),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].LastModified.After(items[j].LastModified)
	})
	folders := make([]string, 0, len(folderCounts))
	for f := range folderCounts {
		folders = append(folders, f)
	}
	sort.Strings(folders)

	writeJSON(w, http.StatusOK, map[string]any{
		"items":   items,
		"folders": folders,
	})
}

// POST /api/silo/rename
func (s *Server) handleRenameSiloObject(w http.ResponseWriter, r *http.Request) {
	if s.cfg.S3 == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Silo not configured"})
		return
	}
	var req struct {
		Key     string `json:"key"`
		NewName string `json:"newName"`
		Force   bool   `json:"force"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	oldKey := strings.TrimLeft(strings.TrimSpace(req.Key), "/")
	cleanName := store.SanitizeFileName(req.NewName)
	if oldKey == "" || cleanName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key and newName required"})
		return
	}
	if isProtectedSiloKey(oldKey) && !req.Force {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "该文件属于博客生产目录（改名会导致博客死链），默认已锁定保护"})
		return
	}
	dir := path.Dir(oldKey)
	newKey := cleanName
	if dir != "." && dir != "/" {
		newKey = dir + "/" + cleanName
	}
	if newKey == oldKey {
		writeJSON(w, http.StatusOK, map[string]any{"key": newKey, "publicUrl": s.cfg.S3.PublicObjectURL(newKey)})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if existing, err := s.cfg.S3.HeadObject(ctx, newKey); err == nil && existing != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "同名文件已存在"})
		return
	}
	if err := s.cfg.S3.CopyObject(ctx, oldKey, newKey); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if err := s.cfg.S3.DeleteObject(ctx, oldKey); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateMediaCache()
	s.broadcast("media.refresh", map[string]string{"key": newKey})
	writeJSON(w, http.StatusOK, map[string]any{
		"key":       newKey,
		"name":      cleanName,
		"publicUrl": s.cfg.S3.PublicObjectURL(newKey),
	})
}

// DELETE /api/silo/object?key=...&force=0
func (s *Server) handleDeleteSiloObject(w http.ResponseWriter, r *http.Request) {
	if s.cfg.S3 == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Silo not configured"})
		return
	}
	key := strings.TrimLeft(strings.TrimSpace(r.URL.Query().Get("key")), "/")
	force := r.URL.Query().Get("force") == "1"
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing key"})
		return
	}
	if isProtectedSiloKey(key) && !force {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "该目录受博客保护锁保护，仅 drop/ 与 images/ 等非博客目录可直接删除"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s.cfg.S3.DeleteObject(ctx, key); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateMediaCache()
	s.broadcast("media.refresh", map[string]string{"deleted": key})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- Google Drive Vault ---

func cleanRelDrivePath(raw string) (string, bool) {
	p := strings.TrimLeft(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"), "/")
	if p == "" || strings.Contains(p, "..") {
		return "", false
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == "/" || strings.HasPrefix(cleaned, "..") {
		return "", false
	}
	return cleaned, true
}

// GET /api/gdrive/files
func (s *Server) handleListGDriveFiles(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GDriveRemoteDir == "" {
		writeJSON(w, http.StatusOK, map[string]any{"items": []GDriveFile{}, "enabled": false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	target := strings.TrimRight(s.cfg.GDriveRemoteDir, "/")
	args := s.rcloneArgs("lsjson", target, "--recursive", "--files-only")
	cmd := exec.CommandContext(ctx, s.cfg.RcloneBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// If remote directory hasn't been created yet on Drive, return empty list cleanly
		if strings.Contains(string(out), "directory not found") || strings.Contains(string(out), "not found") {
			writeJSON(w, http.StatusOK, map[string]any{"items": []GDriveFile{}, "enabled": true})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "读取 Google Drive 列表失败"})
		return
	}
	var rawItems []struct {
		Path     string `json:"Path"`
		Name     string `json:"Name"`
		Size     int64  `json:"Size"`
		MimeType string `json:"MimeType"`
		ModTime  string `json:"ModTime"`
	}
	if err := json.Unmarshal(out, &rawItems); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "解析 Drive 返回失败"})
		return
	}
	items := make([]GDriveFile, 0, len(rawItems))
	for _, it := range rawItems {
		t, _ := time.Parse(time.RFC3339, it.ModTime)
		dir := path.Dir(it.Path)
		if dir == "." {
			dir = "/"
		}
		ext := strings.TrimPrefix(path.Ext(it.Name), ".")
		items = append(items, GDriveFile{
			Path:         it.Path,
			Name:         it.Name,
			Folder:       dir,
			Kind:         classifyAnyExt(ext),
			Ext:          strings.ToUpper(ext),
			Size:         it.Size,
			MimeType:     it.MimeType,
			LastModified: t,
			StreamURL:    "/api/gdrive/stream?path=" + url.QueryEscape(it.Path),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].LastModified.After(items[j].LastModified)
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "enabled": true})
}

// GET /api/gdrive/stream?path=...&download=1
func (s *Server) handleStreamGDriveFile(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GDriveRemoteDir == "" {
		http.NotFound(w, r)
		return
	}
	rel, ok := cleanRelDrivePath(r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	sizeHint, _ := strconv.ParseInt(r.URL.Query().Get("size"), 10, 64)
	s.streamGDriveRel(w, r, rel, path.Base(rel), "", sizeHint, r.URL.Query().Get("download") == "1")
}

func (s *Server) streamGDriveRel(w http.ResponseWriter, r *http.Request, relPath string, fileName string, mimeType string, size int64, download bool) {
	remotePath := strings.TrimRight(s.cfg.GDriveRemoteDir, "/") + "/" + strings.TrimLeft(relPath, "/")
	args := s.rcloneArgs("cat", remotePath)
	cmd := exec.CommandContext(r.Context(), s.cfg.RcloneBin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		http.Error(w, "Google Drive stream error", http.StatusBadGateway)
		return
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(strings.ToLower(path.Ext(fileName)))
	}
	if mimeType != "" {
		w.Header().Set("Content-Type", mimeType)
	}
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	setContentDisposition(w, fileName, download)
	_, _ = io.Copy(w, stdout)
	_ = cmd.Wait()
}

// DELETE /api/gdrive/file?path=...
func (s *Server) handleDeleteGDriveFile(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GDriveRemoteDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Google Drive not configured"})
		return
	}
	rel, ok := cleanRelDrivePath(r.URL.Query().Get("path"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	remotePath := strings.TrimRight(s.cfg.GDriveRemoteDir, "/") + "/" + rel
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.cfg.RcloneBin, s.rcloneArgs("deletefile", remotePath)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "删除 Drive 文件失败: " + strings.TrimSpace(string(out))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/shares/create — universal on-demand share creator for silo / gdrive / local
func (s *Server) handleCreateUniversalShare(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Storage      string `json:"storage"` // "silo" | "gdrive"
		Key          string `json:"key"`
		FileName     string `json:"fileName"`
		Size         int64  `json:"size"`
		TTLHours     int    `json:"ttlHours"`
		MaxDownloads int    `json:"maxDownloads"`
		Passcode     string `json:"passcode"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	var cleanKey string
	switch req.Storage {
	case "silo":
		if s.cfg.S3 == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Silo not configured"})
			return
		}
		cleanKey = strings.TrimLeft(strings.TrimSpace(req.Key), "/")
	case "gdrive":
		if s.cfg.GDriveRemoteDir == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Google Drive not configured"})
			return
		}
		var ok bool
		cleanKey, ok = cleanRelDrivePath(req.Key)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid drive path"})
			return
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported storage"})
		return
	}
	if cleanKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty key"})
		return
	}
	name := strings.TrimSpace(req.FileName)
	if name == "" {
		name = path.Base(cleanKey)
	}
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
		Storage:      req.Storage,
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

// GET /api/music/catalog — reads Aurora Blog's public playlists & cover URLs over internal network!
func (s *Server) handleMusicCatalog(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") == "1"
	s.catalogMu.RLock()
	if !force && !s.catalogFetched.IsZero() && time.Since(s.catalogFetched) < 3*time.Minute && s.catalogCache != nil {
		cp := *s.catalogCache
		s.catalogMu.RUnlock()
		writeJSON(w, http.StatusOK, cp)
		return
	}
	s.catalogMu.RUnlock()

	empty := MusicCatalog{Playlists: []CatalogPlaylist{}, Tracks: map[string]CatalogTrackInfo{}}
	if s.cfg.AuroraAPIBase == "" {
		writeJSON(w, http.StatusOK, empty)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 6 * time.Second}

	base := strings.TrimRight(s.cfg.AuroraAPIBase, "/")
	reqList, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/music/playlists", nil)
	if err != nil {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	respList, err := client.Do(reqList)
	if err != nil {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	defer respList.Body.Close()
	var listWrap struct {
		Data []struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Cover      string `json:"cover"`
			CoverURL   string `json:"coverUrl"`
			TrackCount int    `json:"trackCount"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(respList.Body, 2<<20)).Decode(&listWrap) != nil {
		writeJSON(w, http.StatusOK, empty)
		return
	}

	cat := MusicCatalog{
		Playlists: make([]CatalogPlaylist, 0, len(listWrap.Data)),
		Tracks:    make(map[string]CatalogTrackInfo),
	}

	bucketPrefix := "/aurora/"
	if s.cfg.S3 != nil && s.cfg.S3.Bucket() != "" {
		bucketPrefix = "/" + s.cfg.S3.Bucket() + "/"
	}

	extractSiloKey := func(rawURL string) string {
		if rawURL == "" {
			return ""
		}
		u, err := url.Parse(rawURL)
		if err != nil {
			return ""
		}
		idx := strings.Index(u.Path, bucketPrefix)
		if idx < 0 {
			return ""
		}
		decoded, err := url.PathUnescape(u.Path[idx+len(bucketPrefix):])
		if err != nil {
			return u.Path[idx+len(bucketPrefix):]
		}
		return strings.TrimLeft(decoded, "/")
	}

	for _, pl := range listWrap.Data {
		dReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/music/playlists/%d", base, pl.ID), nil)
		if err != nil {
			continue
		}
		dResp, err := client.Do(dReq)
		if err != nil {
			continue
		}
		var detailWrap struct {
			Data struct {
				Cover  string `json:"cover"`
				Tracks []struct {
					Title      string `json:"title"`
					Artist     string `json:"artist"`
					Album      string `json:"album"`
					Cover      string `json:"cover"`
					CoverURL   string `json:"coverUrl"`
					AudioURL   string `json:"audioUrl"`
					DurationMs int64  `json:"durationMs"`
				} `json:"tracks"`
			} `json:"data"`
		}
		_ = json.NewDecoder(io.LimitReader(dResp.Body, 4<<20)).Decode(&detailWrap)
		dResp.Body.Close()

		keys := make([]string, 0, len(detailWrap.Data.Tracks))
		for _, tr := range detailWrap.Data.Tracks {
			k := extractSiloKey(tr.AudioURL)
			if k == "" {
				continue
			}
			keys = append(keys, k)
			cURL := tr.CoverURL
			if cURL == "" {
				cURL = tr.Cover
			}
			cat.Tracks[k] = CatalogTrackInfo{
				Title:      tr.Title,
				Artist:     tr.Artist,
				Album:      tr.Album,
				CoverURL:   cURL,
				DurationMs: tr.DurationMs,
			}
		}
		plCover := pl.CoverURL
		if plCover == "" {
			plCover = pl.Cover
		}
		if plCover == "" {
			plCover = detailWrap.Data.Cover
		}
		cat.Playlists = append(cat.Playlists, CatalogPlaylist{
			ID:         pl.ID,
			Name:       pl.Name,
			CoverURL:   plCover,
			TrackCount: len(keys),
			TrackKeys:  keys,
		})
	}

	s.catalogMu.Lock()
	s.catalogCache = &cat
	s.catalogFetched = time.Now()
	s.catalogMu.Unlock()

	writeJSON(w, http.StatusOK, cat)
}

func isBlockedDriveName(p string) bool {
	low := strings.ToLower(strings.Trim(p, "/"))
	return strings.HasPrefix(low, "server-restic") || strings.Contains(low, "/server-restic")
}

// normalizeDriveFolderOpt normalizes an optional folder passed to upload init:
// "" -> "" (auto default), "/" -> "/" (OArc-Drop root), "sub/dir" -> "sub/dir".
func normalizeDriveFolderOpt(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" {
		return ""
	}
	if raw == "/" || raw == "." {
		return "/"
	}
	sub, ok := cleanDriveSubfolder(raw)
	if !ok {
		return "/"
	}
	return sub
}

func cleanDriveSubfolder(raw string) (string, bool) {
	p := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	p = strings.Trim(p, "/")
	if p == "" || strings.Contains(p, "..") {
		return "", false
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == "/" || strings.HasPrefix(cleaned, "..") || isBlockedDriveName(cleaned) {
		return "", false
	}
	return cleaned, true
}

// GET /api/gdrive/folders — lists subfolders under OArc-Drop/ (always includes "/" first).
func (s *Server) handleListGDriveFolders(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GDriveRemoteDir == "" {
		writeJSON(w, http.StatusOK, map[string]any{"folders": []string{}, "enabled": false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	target := strings.TrimRight(s.cfg.GDriveRemoteDir, "/")
	args := s.rcloneArgs("lsjson", target, "--recursive", "--dirs-only")
	cmd := exec.CommandContext(ctx, s.cfg.RcloneBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "directory not found") || strings.Contains(string(out), "not found") {
			writeJSON(w, http.StatusOK, map[string]any{"folders": []string{"/"}, "enabled": true})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "读取 Google Drive 目录失败"})
		return
	}
	var rawDirs []struct {
		Path string `json:"Path"`
	}
	if err := json.Unmarshal(out, &rawDirs); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"folders": []string{"/"}, "enabled": true})
		return
	}
	seen := map[string]bool{"/": true}
	subs := make([]string, 0, len(rawDirs))
	for _, d := range rawDirs {
		clean, ok := cleanDriveSubfolder(d.Path)
		if !ok || seen[clean] {
			continue
		}
		seen[clean] = true
		subs = append(subs, clean)
	}
	sort.Strings(subs)
	folders := append([]string{"/"}, subs...)
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders, "enabled": true})
}

// POST /api/gdrive/folders — creates a new subfolder inside OArc-Drop/
func (s *Server) handleCreateGDriveFolder(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GDriveRemoteDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Google Drive 未启用"})
		return
	}
	var req struct {
		Folder string `json:"folder"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	sub, ok := cleanDriveSubfolder(req.Folder)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的文件夹名称"})
		return
	}
	remoteDir := strings.TrimRight(s.cfg.GDriveRemoteDir, "/") + "/" + sub
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.cfg.RcloneBin, s.rcloneArgs("mkdir", remoteDir)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "创建文件夹失败: " + strings.TrimSpace(string(out))})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "folder": sub})
}

// materializeClipLocalPath resolves a local file path for a stored clip so server-side transfers
// (e.g. local -> Silo or local -> Google Drive) require zero browser re-upload.
func (s *Server) materializeClipLocalPath(ctx context.Context, c *store.ClipItem) (string, func(), error) {
	noop := func() {}
	if c.LocalRel != "" {
		full := filepath.Join(s.cfg.DataDir, c.LocalRel)
		if st, err := os.Stat(full); err == nil && !st.IsDir() {
			return full, noop, nil
		}
	}
	tmpPath := filepath.Join(s.cfg.DataDir, "staging", fmt.Sprintf("xfer_%s_%d", c.ID, time.Now().UTC().UnixNano()))
	cleanup := func() { _ = os.Remove(tmpPath) }

	switch c.Storage {
	case "silo":
		if s.cfg.S3 == nil || c.StorageKey == "" {
			return "", noop, fmt.Errorf("Silo source unavailable")
		}
		resp, err := s.cfg.S3.GetObject(ctx, c.StorageKey, "", "")
		if err != nil {
			return "", noop, err
		}
		defer resp.Body.Close()
		f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return "", noop, err
		}
		_, copyErr := io.Copy(f, resp.Body)
		_ = f.Close()
		if copyErr != nil {
			cleanup()
			return "", noop, copyErr
		}
		return tmpPath, cleanup, nil

	case "gdrive":
		if s.cfg.GDriveRemoteDir == "" || c.StorageKey == "" {
			return "", noop, fmt.Errorf("Google Drive source unavailable")
		}
		remotePath := strings.TrimRight(s.cfg.GDriveRemoteDir, "/") + "/" + strings.TrimLeft(c.StorageKey, "/")
		cmd := exec.CommandContext(ctx, s.cfg.RcloneBin, s.rcloneArgs("copyto", remotePath, tmpPath)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			cleanup()
			return "", noop, fmt.Errorf("rclone fetch failed: %s", strings.TrimSpace(string(out)))
		}
		return tmpPath, cleanup, nil
	}
	return "", noop, fmt.Errorf("clip source file not found")
}

// POST /api/clips/{id}/silo — promotes an existing Hub file clip to Silo image host and returns its public URL.
func (s *Server) handlePromoteClipToSilo(w http.ResponseWriter, r *http.Request) {
	if s.cfg.S3 == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Silo 未连接"})
		return
	}
	id := r.PathValue("id")
	c, err := s.store.GetClip(id)
	if err != nil || c.Kind == "text" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "文件不存在"})
		return
	}
	if c.PublicURL != "" {
		writeJSON(w, http.StatusOK, c)
		return
	}
	var req struct {
		Prefix string `json:"prefix"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
	prefix := sanitizeSiloPrefix(req.Prefix, "images/")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	srcPath, cleanup, err := s.materializeClipLocalPath(ctx, c)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "读取源文件失败"})
		return
	}
	defer cleanup()

	f, err := os.Open(srcPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "打开源文件失败"})
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取文件大小失败"})
		return
	}

	mimeType := c.MimeType
	if mimeType == "" {
		mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(c.FileName)))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
	}

	objKey := s.resolveCleanSiloKey(ctx, prefix, c.FileName)
	if err := s.cfg.S3.PutObject(ctx, objKey, f, st.Size(), mimeType, "public, max-age=2592000, immutable"); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "上传图床失败: " + err.Error()})
		return
	}
	pubURL := s.cfg.S3.PublicObjectURL(objKey)
	updated, err := s.store.AttachSiloToClip(id, objKey, pubURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateMediaCache()
	s.broadcast("media.refresh", map[string]string{"key": objKey})
	s.broadcast("clip.updated", updated)
	writeJSON(w, http.StatusOK, updated)
}

// POST /api/clips/{id}/gdrive — copies an existing Hub file clip into a chosen subfolder under OArc-Drop/.
func (s *Server) handleArchiveClipToGDrive(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GDriveRemoteDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Google Drive 未启用"})
		return
	}
	id := r.PathValue("id")
	c, err := s.store.GetClip(id)
	if err != nil || c.Kind == "text" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "文件不存在"})
		return
	}
	var req struct {
		Folder string `json:"folder"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
	rawFolder := strings.TrimSpace(req.Folder)

	var relKey string
	if rawFolder == "" || rawFolder == "/" || rawFolder == "." {
		relKey = c.FileName
	} else {
		sub, ok := cleanDriveSubfolder(rawFolder)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的 Google Drive 文件夹"})
			return
		}
		relKey = sub + "/" + c.FileName
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Minute)
	defer cancel()

	srcPath, cleanup, err := s.materializeClipLocalPath(ctx, c)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "读取源文件失败"})
		return
	}
	defer cleanup()

	dest := strings.TrimRight(s.cfg.GDriveRemoteDir, "/") + "/" + relKey
	cmd := exec.CommandContext(ctx, s.cfg.RcloneBin, s.rcloneArgs("copyto", srcPath, dest)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "存入 Google Drive 失败: " + strings.TrimSpace(string(out))})
		return
	}
	updated, err := s.store.AttachGDriveToClip(id, relKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcast("clip.updated", updated)
	writeJSON(w, http.StatusOK, updated)
}
