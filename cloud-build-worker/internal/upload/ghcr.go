package upload

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type GHCRConfig struct {
	Owner string
	Token string
}

func NewGHCR(cfg GHCRConfig) Uploader {
	return ghcrUploader{cfg: cfg, client: http.DefaultClient, base: ghcrBaseURL}
}

type ghcrUploader struct {
	cfg    GHCRConfig
	client *http.Client
	base   string
}

func (ghcrUploader) Type() string {
	return "ghcr"
}

var ghcrBaseURL = "https://ghcr.io"

func (u ghcrUploader) Upload(ctx context.Context, r io.ReadSeeker, opts Options) (Result, error) {
	offset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return Result{}, err
	}

	result, err := checksumResult(r)
	if err != nil {
		return Result{}, err
	}

	repo := u.repository(opts.Name)
	uploadURL, err := u.startUpload(ctx, repo)
	if err != nil {
		_, _ = r.Seek(offset, io.SeekStart)
		return Result{}, err
	}
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return Result{}, err
	}
	if err := u.completeUpload(ctx, uploadURL, result.Checksum, r, offset); err != nil {
		_, _ = r.Seek(offset, io.SeekStart)
		return Result{}, err
	}
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return Result{}, err
	}
	digest := result.Checksum[len("sha256:"):]
	result.URL = fmt.Sprintf("%s/v2/%s/blobs/sha256:%s", strings.TrimRight(u.baseURL(), "/"), repo, digest)
	return result, nil
}

func checksumResult(r io.ReadSeeker) (Result, error) {
	offset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return Result{}, err
	}

	h := sha256.New()
	size, err := io.Copy(h, r)
	_, restoreErr := r.Seek(offset, io.SeekStart)
	if err != nil {
		return Result{}, err
	}
	if restoreErr != nil {
		return Result{}, restoreErr
	}
	return Result{
		Size:     size,
		Checksum: "sha256:" + hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func (u ghcrUploader) baseURL() string {
	if u.base != "" {
		return u.base
	}
	return ghcrBaseURL
}

func (u ghcrUploader) httpClient() *http.Client {
	if u.client != nil {
		return u.client
	}
	return http.DefaultClient
}

func (u ghcrUploader) repository(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "ghcr.io/")
	if repo, _, ok := strings.Cut(name, ":"); ok {
		name = repo
	}
	name = strings.Trim(name, "/")
	if u.cfg.Owner == "" {
		return name
	}
	owner := strings.Trim(u.cfg.Owner, "/")
	if name == owner || strings.HasPrefix(name, owner+"/") {
		return name
	}
	return owner + "/" + name
}

func (u ghcrUploader) startUpload(ctx context.Context, repo string) (string, error) {
	endpoint := fmt.Sprintf("%s/v2/%s/blobs/uploads/", strings.TrimRight(u.baseURL(), "/"), repo)
	resp, err := u.doRegistryRequest(ctx, func(token string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
		if err != nil {
			return nil, err
		}
		authorize(req, token)
		return req, nil
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("start ghcr upload: %s", resp.Status)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("start ghcr upload: missing Location header")
	}
	return resolveLocation(endpoint, location)
}

func (u ghcrUploader) completeUpload(ctx context.Context, uploadURL, digest string, r io.ReadSeeker, offset int64) error {
	endpoint, err := appendDigest(uploadURL, digest)
	if err != nil {
		return err
	}
	resp, err := u.doRegistryRequest(ctx, func(token string) (*http.Request, error) {
		if _, err := r.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, ctxReader{ctx: ctx, r: r})
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		authorize(req, token)
		return req, nil
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("complete ghcr upload: %s", resp.Status)
	}
	return nil
}

func (u ghcrUploader) doRegistryRequest(ctx context.Context, newRequest func(token string) (*http.Request, error)) (*http.Response, error) {
	req, err := newRequest(u.cfg.Token)
	if err != nil {
		return nil, err
	}
	resp, err := u.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	challenge := resp.Header.Get("WWW-Authenticate")
	resp.Body.Close()
	token, err := u.challengeToken(ctx, challenge)
	if err != nil {
		return nil, err
	}
	req, err = newRequest(token)
	if err != nil {
		return nil, err
	}
	return u.httpClient().Do(req)
}

func (u ghcrUploader) challengeToken(ctx context.Context, header string) (string, error) {
	challenge, err := parseBearerChallenge(header)
	if err != nil {
		return "", err
	}
	endpoint, err := url.Parse(challenge["realm"])
	if err != nil {
		return "", err
	}
	query := endpoint.Query()
	for _, key := range []string{"service", "scope"} {
		if value := challenge[key]; value != "" {
			query.Set(key, value)
		}
	}
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	if u.cfg.Token != "" {
		user := u.cfg.Owner
		if user == "" {
			user = "llar"
		}
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+u.cfg.Token)))
	}

	resp, err := u.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ghcr token: %s", resp.Status)
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.Token != "" {
		return body.Token, nil
	}
	if body.AccessToken != "" {
		return body.AccessToken, nil
	}
	return "", fmt.Errorf("ghcr token: missing token")
}

func authorize(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func resolveLocation(baseURL, location string) (string, error) {
	loc, err := url.Parse(location)
	if err != nil {
		return "", err
	}
	if loc.IsAbs() {
		return loc.String(), nil
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(loc).String(), nil
}

func appendDigest(uploadURL, digest string) (string, error) {
	u, err := url.Parse(uploadURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("digest", digest)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func parseBearerChallenge(header string) (map[string]string, error) {
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, fmt.Errorf("unsupported registry auth challenge")
	}
	values := map[string]string{}
	for _, part := range splitChallengeParams(strings.TrimPrefix(header, "Bearer ")) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	if values["realm"] == "" {
		return nil, fmt.Errorf("registry auth challenge missing realm")
	}
	return values, nil
}

func splitChallengeParams(input string) []string {
	var parts []string
	start := 0
	inQuotes := false
	for i, ch := range input {
		switch ch {
		case '"':
			inQuotes = !inQuotes
		case ',':
			if !inQuotes {
				parts = append(parts, strings.TrimSpace(input[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(input[start:]))
	return parts
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r ctxReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.r.Read(p)
	}
}
