package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"oarc-drop/internal/s3client"
	"oarc-drop/internal/server"
	"oarc-drop/internal/store"
)

func main() {
	listenAddr := envOr("LISTEN_ADDR", ":8095")
	dataDir := envOr("DATA_DIR", "./data")
	webDir := envOr("WEB_DIR", "./web/dist")
	maxClips, _ := strconv.Atoi(envOr("MAX_CLIPS", "200"))

	st, err := store.New(dataDir, maxClips)
	if err != nil {
		log.Fatalf("[oarc-drop] init store: %v", err)
	}

	var s3c *s3client.Client
	s3Endpoint := strings.TrimSpace(os.Getenv("S3_ENDPOINT"))
	if s3Endpoint != "" {
		s3c, err = s3client.New(s3client.Config{
			Endpoint:  s3Endpoint,
			Region:    envOr("S3_REGION", "us-east-1"),
			AccessKey: os.Getenv("S3_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_SECRET_KEY"),
			Bucket:    envOr("S3_BUCKET", "aurora"),
			PublicURL: envOr("S3_PUBLIC_URL", "https://img.arcueidblog.top"),
		})
		if err != nil {
			log.Fatalf("[oarc-drop] init s3 client: %v", err)
		}
		log.Printf("[oarc-drop] Silo S3 connected: endpoint=%s bucket=%s", s3Endpoint, s3c.Bucket())
	}

	var staticFS fs.FS
	if info, err := os.Stat(webDir); err == nil && info.IsDir() {
		staticFS = os.DirFS(webDir)
	}

	headerLinks := defaultHeaderLinks()
	if rawLinks := strings.TrimSpace(os.Getenv("HEADER_LINKS_JSON")); rawLinks != "" {
		var parsed []server.HeaderLink
		if json.Unmarshal([]byte(rawLinks), &parsed) == nil {
			headerLinks = parsed
		}
	}

	srv := server.New(server.Config{
		DataDir:         dataDir,
		S3:              s3c,
		SiloDropPrefix:  envOr("SILO_DROP_PREFIX", "drop/"),
		GDriveRemoteDir: strings.TrimSpace(os.Getenv("GDRIVE_REMOTE_DIR")),
		RcloneBin:       envOr("RCLONE_BIN", "rclone"),
		RcloneConfig:    strings.TrimSpace(os.Getenv("RCLONE_CONFIG")),
		RequireCFAccess: strings.EqualFold(os.Getenv("REQUIRE_CF_ACCESS"), "true") || os.Getenv("REQUIRE_CF_ACCESS") == "1",
		AdminToken:      strings.TrimSpace(os.Getenv("ADMIN_TOKEN")),
		PublicBaseURL:   strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"),
		HeaderLinks:     headerLinks,
		StaticFS:        staticFS,
	}, st)

	// Background janitor for expired shares & stale upload staging
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n := st.SweepExpired(); n > 0 {
					log.Printf("[oarc-drop] swept %d expired share(s)", n)
				}
			}
		}
	}()

	httpSrv := &http.Server{
		Addr:              listenAddr,
		Handler:           srv,
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("[oarc-drop] listening on %s (data=%s)", listenAddr, dataDir)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[oarc-drop] listen failed: %v", err)
		}
	}()

	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutCtx)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func defaultHeaderLinks() []server.HeaderLink {
	return []server.HeaderLink{
		{Label: "Blog", URL: "https://arcueidblog.top"},
		{Label: "Monitor", URL: "https://monitor.arcueidblog.top"},
		{Label: "SIM", URL: "https://sim.arcueidblog.top"},
	}
}
