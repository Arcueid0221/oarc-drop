package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrExpired  = errors.New("expired or download limit reached")
)

type ClipItem struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"` // "text" | "image" | "file"
	Text       string    `json:"text,omitempty"`
	FileName   string    `json:"fileName,omitempty"`
	MimeType   string    `json:"mimeType,omitempty"`
	Size       int64     `json:"size,omitempty"`
	SHA256     string    `json:"sha256,omitempty"`
	Storage    string    `json:"storage,omitempty"`    // "local" | "silo" | "gdrive"
	StorageKey string    `json:"storageKey,omitempty"` // S3 key or Drive rel path (omitted for local internal path)
	PublicURL  string    `json:"publicUrl,omitempty"`  // Direct CDN URL when stored/promoted in Silo
	SiloKey    string    `json:"siloKey,omitempty"`    // Silo object key when promoted to image host
	GDrivePath string    `json:"gdrivePath,omitempty"` // Relative path under OArc-Drop/ when archived to Google Drive
	LocalRel   string    `json:"-"`                    // relative to DataDir when Storage == "local"
	ShareCode  string    `json:"shareCode,omitempty"`
	Pinned     bool      `json:"pinned"`
	CreatedAt  time.Time `json:"createdAt"`
}

type ShareItem struct {
	Code         string     `json:"code"`
	FileName     string     `json:"fileName"`
	MimeType     string     `json:"mimeType"`
	Size         int64      `json:"size"`
	SHA256       string     `json:"sha256,omitempty"`
	Storage      string     `json:"storage"` // "local" | "silo" | "gdrive"
	StorageKey   string     `json:"storageKey"`
	CreatedAt    time.Time  `json:"createdAt"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	MaxDownloads int        `json:"maxDownloads,omitempty"`
	Downloads    int        `json:"downloads"`
	HasPasscode  bool       `json:"hasPasscode"`
	PassSalt     string     `json:"passSalt,omitempty"`
	PassHash     string     `json:"passHash,omitempty"`
}

type UploadSession struct {
	ID           string    `json:"id"`
	FileName     string    `json:"fileName"`
	MimeType     string    `json:"mimeType"`
	Size         int64     `json:"size"`
	ChunkSize    int64     `json:"chunkSize"`
	TotalParts   int       `json:"totalParts"`
	PartsDone    map[int]int64 `json:"partsDone"`
	Target       string    `json:"target"` // "local" | "silo" | "gdrive"
	SiloPrefix   string    `json:"siloPrefix,omitempty"`
	GDriveFolder string    `json:"gdriveFolder,omitempty"`
	TTLHours     int       `json:"ttlHours"`
	MaxDownloads int       `json:"maxDownloads"`
	Passcode     string    `json:"-"` // transient until complete
	PassSalt     string    `json:"passSalt,omitempty"`
	PassHash     string    `json:"passHash,omitempty"`
	AddToClip    bool      `json:"addToClip,omitempty"`
	SkipShare    bool      `json:"skipShare,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type persistedState struct {
	Clips      []*ClipItem     `json:"clips"`
	Shares     []*ShareItem    `json:"shares"`
	SecretSeed string          `json:"secretSeed"`
}

type Store struct {
	mu         sync.RWMutex
	dataDir    string
	maxClips   int
	clips      map[string]*ClipItem
	shares     map[string]*ShareItem
	uploads    map[string]*UploadSession
	secretSeed []byte
}

func New(dataDir string, maxClips int) (*Store, error) {
	if maxClips <= 0 {
		maxClips = 200
	}
	for _, sub := range []string{"clips", "files", "staging", "cache"} {
		if err := os.MkdirAll(filepath.Join(dataDir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}
	s := &Store{
		dataDir:  dataDir,
		maxClips: maxClips,
		clips:    make(map[string]*ClipItem),
		shares:   make(map[string]*ShareItem),
		uploads:  make(map[string]*UploadSession),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) DataDir() string { return s.dataDir }

func (s *Store) SecretSeed() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]byte(nil), s.secretSeed...)
}

func (s *Store) load() error {
	p := filepath.Join(s.dataDir, "state.json")
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		seed := make([]byte, 32)
		_, _ = rand.Read(seed)
		s.secretSeed = seed
		return s.saveLocked()
	}
	if err != nil {
		return fmt.Errorf("read state.json: %w", err)
	}
	var ds diskState
	if err := json.Unmarshal(raw, &ds); err != nil {
		return fmt.Errorf("parse state.json: %w", err)
	}
	for _, dc := range ds.Clips {
		if dc.ID != "" {
			cp := dc.ClipItem
			if cp.Storage == "" && cp.Kind != "text" {
				cp.Storage = "local"
			}
			if dc.LocalRel != "" {
				cp.LocalRel = dc.LocalRel
			} else if cp.Kind != "text" && cp.Storage == "local" && cp.FileName != "" {
				cp.LocalRel = filepath.Join("clips", cp.ID+"_"+sanitizeFileName(cp.FileName))
			}
			s.clips[cp.ID] = &cp
		}
	}
	for _, sh := range ds.Shares {
		if sh != nil && sh.Code != "" {
			sh.HasPasscode = sh.PassHash != ""
			s.shares[sh.Code] = sh
		}
	}
	if seed, err := hex.DecodeString(ds.SecretSeed); err == nil && len(seed) >= 16 {
		s.secretSeed = seed
	} else {
		seed := make([]byte, 32)
		_, _ = rand.Read(seed)
		s.secretSeed = seed
	}
	return nil
}

// persistedClip includes LocalRel on disk without exposing internal paths over public JSON if marshaled separately.
type diskState struct {
	Clips      []diskClip   `json:"clips"`
	Shares     []*ShareItem `json:"shares"`
	SecretSeed string       `json:"secretSeed"`
}

type diskClip struct {
	ClipItem
	LocalRel string `json:"localRel,omitempty"`
}

func (s *Store) loadV2Fallback(raw []byte) {
	var ds diskState
	if json.Unmarshal(raw, &ds) == nil {
		for _, dc := range ds.Clips {
			if c := s.clips[dc.ID]; c != nil && dc.LocalRel != "" {
				c.LocalRel = dc.LocalRel
			}
		}
	}
}

func (s *Store) saveLocked() error {
	clips := make([]diskClip, 0, len(s.clips))
	for _, c := range s.clips {
		clips = append(clips, diskClip{ClipItem: *c, LocalRel: c.LocalRel})
	}
	sort.Slice(clips, func(i, j int) bool {
		return clips[i].CreatedAt.After(clips[j].CreatedAt)
	})
	shares := make([]*ShareItem, 0, len(s.shares))
	for _, sh := range s.shares {
		shares = append(shares, sh)
	}
	sort.Slice(shares, func(i, j int) bool {
		return shares[i].CreatedAt.After(shares[j].CreatedAt)
	})
	ds := diskState{
		Clips:      clips,
		Shares:     shares,
		SecretSeed: hex.EncodeToString(s.secretSeed),
	}
	raw, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dataDir, "state.json.tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	_ = f.Sync()
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dataDir, "state.json"))
}

func (s *Store) ListClips() []*ClipItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ClipItem, 0, len(s.clips))
	for _, c := range s.clips {
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *Store) GetClip(id string) (*ClipItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.clips[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (s *Store) AddTextClip(text string) (*ClipItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sum := sha256.Sum256([]byte(text))
	item := &ClipItem{
		ID:        randomID(10),
		Kind:      "text",
		Text:      text,
		Size:      int64(len(text)),
		SHA256:    hex.EncodeToString(sum[:8]),
		CreatedAt: time.Now().UTC(),
	}
	s.clips[item.ID] = item
	s.pruneClipsLocked()
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	cp := *item
	return &cp, nil
}

func (s *Store) AddFileClip(id string, kind string, fileName string, mimeType string, size int64, shaHex string, localRel string, shareCode string) (*ClipItem, error) {
	return s.AddStoredFileClip(ClipItem{
		ID:        id,
		Kind:      kind,
		FileName:  fileName,
		MimeType:  mimeType,
		Size:      size,
		SHA256:    shaHex,
		Storage:   "local",
		LocalRel:  localRel,
		ShareCode: shareCode,
	})
}

func (s *Store) AddStoredFileClip(item ClipItem) (*ClipItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.ID == "" {
		item.ID = randomID(10)
	}
	if item.Storage == "" {
		item.Storage = "local"
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	stored := item
	s.clips[stored.ID] = &stored
	s.pruneClipsLocked()
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	cp := stored
	return &cp, nil
}

func (s *Store) ListLocalFiles() []ClipItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ClipItem, 0)
	for _, c := range s.clips {
		if c.Kind != "text" && (c.Storage == "" || c.Storage == "local") && c.LocalRel != "" {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *Store) SetClipPinned(id string, pinned bool) (*ClipItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clips[id]
	if !ok {
		return nil, ErrNotFound
	}
	c.Pinned = pinned
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	cp := *c
	return &cp, nil
}

func (s *Store) AttachShareToClip(id string, shareCode string) (*ClipItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clips[id]
	if !ok {
		return nil, ErrNotFound
	}
	c.ShareCode = shareCode
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	cp := *c
	return &cp, nil
}

func (s *Store) AttachSiloToClip(id string, siloKey string, publicURL string) (*ClipItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clips[id]
	if !ok {
		return nil, ErrNotFound
	}
	c.SiloKey = siloKey
	c.PublicURL = publicURL
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	cp := *c
	return &cp, nil
}

func (s *Store) AttachGDriveToClip(id string, gdrivePath string) (*ClipItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clips[id]
	if !ok {
		return nil, ErrNotFound
	}
	c.GDrivePath = gdrivePath
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	cp := *c
	return &cp, nil
}

func (s *Store) DeleteClip(id string) error {
	s.mu.Lock()
	c, ok := s.clips[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	localRel := c.LocalRel
	stillReferenced := s.isLocalRelReferencedByShareLocked(localRel)
	delete(s.clips, id)
	err := s.saveLocked()
	s.mu.Unlock()
	if localRel != "" && !stillReferenced {
		_ = os.Remove(filepath.Join(s.dataDir, localRel))
	}
	return err
}

func (s *Store) pruneClipsLocked() {
	if len(s.clips) <= s.maxClips {
		return
	}
	unpinned := make([]*ClipItem, 0, len(s.clips))
	for _, c := range s.clips {
		if !c.Pinned {
			unpinned = append(unpinned, c)
		}
	}
	sort.Slice(unpinned, func(i, j int) bool {
		return unpinned[i].CreatedAt.Before(unpinned[j].CreatedAt)
	})
	for len(s.clips) > s.maxClips && len(unpinned) > 0 {
		victim := unpinned[0]
		unpinned = unpinned[1:]
		delete(s.clips, victim.ID)
		if victim.LocalRel != "" && !s.isLocalRelReferencedByShareLocked(victim.LocalRel) {
			_ = os.Remove(filepath.Join(s.dataDir, victim.LocalRel))
		}
	}
}

func (s *Store) isLocalRelReferencedByShareLocked(rel string) bool {
	if rel == "" {
		return false
	}
	for _, sh := range s.shares {
		if sh.Storage == "local" && sh.StorageKey == rel {
			return true
		}
	}
	return false
}

func (s *Store) isLocalRelReferencedByClipLocked(rel string) bool {
	if rel == "" {
		return false
	}
	for _, c := range s.clips {
		if c.LocalRel == rel {
			return true
		}
	}
	return false
}

func (s *Store) CreateShare(item ShareItem, plainPasscode string) (*ShareItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.Code == "" {
		for {
			cand := randomCode(7)
			if _, exists := s.shares[cand]; !exists {
				item.Code = cand
				break
			}
		}
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	plainPasscode = strings.TrimSpace(plainPasscode)
	if plainPasscode != "" {
		salt := randomID(8)
		item.PassSalt = salt
		item.PassHash = hashPasscode(salt, plainPasscode)
		item.HasPasscode = true
	}
	s.shares[item.Code] = &item
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return sanitizeShare(&item), nil
}

func (s *Store) ListShares() []*ShareItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().UTC()
	out := make([]*ShareItem, 0, len(s.shares))
	for _, sh := range s.shares {
		if isShareExpired(sh, now) {
			continue
		}
		out = append(out, sanitizeShare(sh))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *Store) GetShare(code string) (*ShareItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sh, ok := s.shares[code]
	if !ok {
		return nil, ErrNotFound
	}
	if isShareExpired(sh, time.Now().UTC()) {
		return nil, ErrExpired
	}
	cp := *sh
	return &cp, nil
}

func (s *Store) VerifySharePasscode(code string, candidate string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sh, ok := s.shares[code]
	if !ok || isShareExpired(sh, time.Now().UTC()) {
		return false
	}
	if !sh.HasPasscode {
		return true
	}
	got := hashPasscode(sh.PassSalt, strings.TrimSpace(candidate))
	return subtle.ConstantTimeCompare([]byte(got), []byte(sh.PassHash)) == 1
}

func (s *Store) IncrementShareDownload(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.shares[code]
	if !ok {
		return ErrNotFound
	}
	if isShareExpired(sh, time.Now().UTC()) {
		return ErrExpired
	}
	sh.Downloads++
	return s.saveLocked()
}

func (s *Store) DeleteShare(code string) (*ShareItem, bool, error) {
	s.mu.Lock()
	sh, ok := s.shares[code]
	if !ok {
		s.mu.Unlock()
		return nil, false, ErrNotFound
	}
	cp := *sh
	delete(s.shares, code)
	canDeleteLocal := cp.Storage == "local" && !s.isLocalRelReferencedByClipLocked(cp.StorageKey) && !s.isLocalRelReferencedByShareLocked(cp.StorageKey)
	err := s.saveLocked()
	s.mu.Unlock()
	if canDeleteLocal && cp.StorageKey != "" {
		_ = os.Remove(filepath.Join(s.dataDir, cp.StorageKey))
	}
	return sanitizeShare(&cp), canDeleteLocal, err
}

// Upload sessions (in-memory + staging files on disk)

func (s *Store) CreateUploadSession(u *UploadSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u.ID == "" {
		u.ID = randomID(12)
	}
	if u.PartsDone == nil {
		u.PartsDone = make(map[int]int64)
	}
	u.CreatedAt = time.Now().UTC()
	s.uploads[u.ID] = u
}

func (s *Store) GetUploadSession(id string) (*UploadSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.uploads[id]
	if !ok {
		return nil, false
	}
	cp := *u
	cp.PartsDone = make(map[int]int64, len(u.PartsDone))
	for k, v := range u.PartsDone {
		cp.PartsDone[k] = v
	}
	return &cp, true
}

func (s *Store) MarkUploadPart(id string, part int, n int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.uploads[id]
	if !ok {
		return ErrNotFound
	}
	u.PartsDone[part] = n
	return nil
}

func (s *Store) PopUploadSession(id string) (*UploadSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.uploads[id]
	if !ok {
		return nil, false
	}
	delete(s.uploads, id)
	return u, true
}

func (s *Store) SweepExpired() int {
	s.mu.Lock()
	now := time.Now().UTC()
	var localToDelete []string
	removed := 0
	for code, sh := range s.shares {
		if isShareExpired(sh, now) {
			delete(s.shares, code)
			removed++
			if sh.Storage == "local" && sh.StorageKey != "" &&
				!s.isLocalRelReferencedByClipLocked(sh.StorageKey) &&
				!s.isLocalRelReferencedByShareLocked(sh.StorageKey) {
				localToDelete = append(localToDelete, sh.StorageKey)
			}
		}
	}
	var staleUploads []string
	for id, u := range s.uploads {
		if now.Sub(u.CreatedAt) > 12*time.Hour {
			delete(s.uploads, id)
			staleUploads = append(staleUploads, id)
		}
	}
	if removed > 0 {
		_ = s.saveLocked()
	}
	s.mu.Unlock()

	for _, rel := range localToDelete {
		_ = os.Remove(filepath.Join(s.dataDir, rel))
	}
	for _, id := range staleUploads {
		_ = os.RemoveAll(filepath.Join(s.dataDir, "staging", id))
	}
	return removed
}

func isShareExpired(sh *ShareItem, now time.Time) bool {
	if sh.ExpiresAt != nil && now.After(*sh.ExpiresAt) {
		return true
	}
	if sh.MaxDownloads > 0 && sh.Downloads >= sh.MaxDownloads {
		return true
	}
	return false
}

func sanitizeShare(sh *ShareItem) *ShareItem {
	cp := *sh
	cp.HasPasscode = sh.PassHash != ""
	cp.PassSalt = ""
	cp.PassHash = ""
	return &cp
}

func hashPasscode(salt, passcode string) string {
	sum := sha256.Sum256([]byte(salt + ":" + passcode))
	return hex.EncodeToString(sum[:])
}

func sanitizeFileName(name string) string {
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	base = strings.Map(func(r rune) rune {
		if r < 32 || r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, base)
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == ".." {
		return "file.bin"
	}
	return base
}

func SanitizeFileName(name string) string { return sanitizeFileName(name) }

func randomID(byteLen int) string {
	b := make([]byte, byteLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

const codeAlphabet = "23456789abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ"

func randomCode(length int) string {
	b := make([]byte, length)
	_, _ = rand.Read(b)
	out := make([]byte, length)
	for i := range b {
		out[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(out)
}
