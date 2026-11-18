package s3client

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Config struct {
	Endpoint  string // e.g. http://minio:9000
	Region    string // default us-east-1
	AccessKey string
	SecretKey string
	Bucket    string
	PublicURL string            // e.g. https://img.arcueidblog.top
	Transport http.RoundTripper // optional custom transport for testing
}

type Client struct {
	cfg        Config
	endpoint   *url.URL
	httpClient *http.Client
}

type ObjectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag"`
	LastModified time.Time `json:"lastModified"`
}

func New(cfg Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("s3 endpoint is required")
	}
	u, err := url.Parse(strings.TrimRight(cfg.Endpoint, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse s3 endpoint: %w", err)
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	tr := cfg.Transport
	if tr == nil {
		tr = &http.Transport{
			MaxIdleConns:        64,
			MaxIdleConnsPerHost: 32,
			IdleConnTimeout:     90 * time.Second,
		}
	}
	return &Client{
		cfg:      cfg,
		endpoint: u,
		httpClient: &http.Client{
			Timeout:   0, // streaming requests use context deadlines
			Transport: tr,
		},
	}, nil
}

func (c *Client) Bucket() string { return c.cfg.Bucket }

func (c *Client) PublicObjectURL(key string) string {
	clean := strings.TrimLeft(key, "/")
	if c.cfg.PublicURL == "" {
		return ""
	}
	parts := strings.Split(clean, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return c.cfg.PublicURL + "/" + url.PathEscape(c.cfg.Bucket) + "/" + strings.Join(parts, "/")
}

type listBucketResultV2 struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	IsTruncated           bool     `xml:"IsTruncated"`
	NextContinuationToken string   `xml:"NextContinuationToken"`
	Contents              []struct {
		Key          string `xml:"Key"`
		LastModified string `xml:"LastModified"`
		ETag         string `xml:"ETag"`
		Size         int64  `xml:"Size"`
	} `xml:"Contents"`
}

func (c *Client) ListAll(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	var out []ObjectInfo
	token := ""
	for {
		q := url.Values{}
		q.Set("list-type", "2")
		q.Set("max-keys", "1000")
		if prefix != "" {
			q.Set("prefix", prefix)
		}
		if token != "" {
			q.Set("continuation-token", token)
		}
		req, err := c.newSignedRequest(ctx, http.MethodGet, "", q, nil, 0, "")
		if err != nil {
			return nil, err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("s3 list status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var parsed listBucketResultV2
		if err := xml.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("decode s3 list xml: %w", err)
		}
		for _, item := range parsed.Contents {
			t, _ := time.Parse(time.RFC3339, item.LastModified)
			out = append(out, ObjectInfo{
				Key:          item.Key,
				Size:         item.Size,
				ETag:         strings.Trim(item.ETag, "\""),
				LastModified: t,
			})
		}
		if !parsed.IsTruncated || parsed.NextContinuationToken == "" {
			break
		}
		token = parsed.NextContinuationToken
	}
	return out, nil
}

func (c *Client) GetObject(ctx context.Context, key string, rangeHeader string, ifNoneMatch string) (*http.Response, error) {
	req, err := c.newSignedRequest(ctx, http.MethodGet, key, nil, nil, 0, "")
	if err != nil {
		return nil, err
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotModified {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("s3 get %q status %d: %s", key, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return resp, nil
}

func (c *Client) PutObject(ctx context.Context, key string, body io.Reader, size int64, contentType string, cacheControl string) error {
	extra := map[string]string{}
	if cacheControl != "" {
		extra["Cache-Control"] = cacheControl
	}
	req, err := c.newSignedRequestWithHeaders(ctx, http.MethodPut, key, nil, body, size, contentType, extra)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("s3 put %q status %d: %s", key, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

func (c *Client) DeleteObject(ctx context.Context, key string) error {
	req, err := c.newSignedRequest(ctx, http.MethodDelete, key, nil, nil, 0, "")
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("s3 delete %q status %d: %s", key, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

type initiateMultipartResult struct {
	UploadId string `xml:"UploadId"`
}

type CompletedPart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

type completeMultipartUploadReq struct {
	XMLName xml.Name        `xml:"CompleteMultipartUpload"`
	Parts   []CompletedPart `xml:"Part"`
}

func (c *Client) CreateMultipartUpload(ctx context.Context, key string, contentType string, cacheControl string) (string, error) {
	q := url.Values{"uploads": {""}}
	extra := map[string]string{}
	if cacheControl != "" {
		extra["Cache-Control"] = cacheControl
	}
	req, err := c.newSignedRequestWithHeaders(ctx, http.MethodPost, key, q, nil, 0, contentType, extra)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("s3 init multipart %q status %d: %s", key, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed initiateMultipartResult
	if err := xml.Unmarshal(raw, &parsed); err != nil || parsed.UploadId == "" {
		return "", fmt.Errorf("decode s3 uploadId: %w", err)
	}
	return parsed.UploadId, nil
}

func (c *Client) UploadPart(ctx context.Context, key string, uploadID string, partNumber int, body io.Reader, size int64) (string, error) {
	q := url.Values{
		"partNumber": {fmt.Sprintf("%d", partNumber)},
		"uploadId":   {uploadID},
	}
	req, err := c.newSignedRequest(ctx, http.MethodPut, key, q, body, size, "")
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("s3 upload part %d status %d: %s", partNumber, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		return "", errors.New("s3 upload part missing ETag")
	}
	return etag, nil
}

func (c *Client) CompleteMultipartUpload(ctx context.Context, key string, uploadID string, parts []CompletedPart) error {
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	payload, err := xml.Marshal(completeMultipartUploadReq{Parts: parts})
	if err != nil {
		return err
	}
	q := url.Values{"uploadId": {uploadID}}
	req, err := c.newSignedRequest(ctx, http.MethodPost, key, q, bytes.NewReader(payload), int64(len(payload)), "application/xml")
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("s3 complete multipart status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (c *Client) AbortMultipartUpload(ctx context.Context, key string, uploadID string) error {
	q := url.Values{"uploadId": {uploadID}}
	req, err := c.newSignedRequest(ctx, http.MethodDelete, key, q, nil, 0, "")
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) newSignedRequest(ctx context.Context, method string, key string, query url.Values, body io.Reader, contentLength int64, contentType string) (*http.Request, error) {
	return c.newSignedRequestWithHeaders(ctx, method, key, query, body, contentLength, contentType, nil)
}

func (c *Client) newSignedRequestWithHeaders(ctx context.Context, method string, key string, query url.Values, body io.Reader, contentLength int64, contentType string, extraHeaders map[string]string) (*http.Request, error) {
	escapedPath := "/" + s3EscapePath(c.cfg.Bucket)
	if cleanKey := strings.TrimLeft(key, "/"); cleanKey != "" {
		escapedPath += "/" + s3EscapePath(cleanKey)
	}
	basePath := strings.TrimRight(c.endpoint.EscapedPath(), "/")
	fullEscapedPath := basePath + escapedPath

	u := *c.endpoint
	u.Path, _ = url.PathUnescape(fullEscapedPath)
	u.RawPath = fullEscapedPath
	if len(query) > 0 {
		u.RawQuery = canonicalQueryString(query)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if contentLength >= 0 && body != nil {
		req.ContentLength = contentLength
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range extraHeaders {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := "UNSIGNED-PAYLOAD"
	if body == nil {
		// Empty body SHA-256
		payloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	}

	req.Header.Set("Host", c.endpoint.Host)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	signedHeaders := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n",
		c.endpoint.Host, payloadHash, amzDate)

	canonicalReq := strings.Join([]string{
		method,
		fullEscapedPath,
		canonicalQueryString(query),
		canonicalHeaders,
		strings.Join(signedHeaders, ";"),
		payloadHash,
	}, "\n")

	scope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, c.cfg.Region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalReq)),
	}, "\n")

	signingKey := deriveSigningKey(c.cfg.SecretKey, dateStamp, c.cfg.Region, "s3")
	sig := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.cfg.AccessKey, scope, strings.Join(signedHeaders, ";"), sig)
	req.Header.Set("Authorization", authHeader)

	return req, nil
}

func s3EscapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = s3EscapeComponent(s)
	}
	return strings.Join(segs, "/")
}

func s3EscapeComponent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') ||
			ch == '-' || ch == '_' || ch == '.' || ch == '~' {
			b.WriteByte(ch)
		} else {
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

func canonicalQueryString(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		ek := s3EscapeComponent(k)
		for _, v := range vals {
			pairs = append(pairs, ek+"="+s3EscapeComponent(v))
		}
	}
	return strings.Join(pairs, "&")
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func deriveSigningKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}
