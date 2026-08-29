package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	codexLatestReleaseURL         = "https://api.github.com/repos/openai/codex/releases/latest"
	codexClientVersionLookupLimit = 5 * time.Second
	codexClientVersionCacheTTL    = time.Hour
	codexClientVersionFallback    = "0.150.1"
	maxCodexReleaseResponseBytes  = 1 << 20
	maxCodexModelsResponseBytes   = 10 << 20
)

type codexClientVersionCache struct {
	sync.Mutex
	version   string
	expiresAt time.Time
}

var latestCodexClientVersion codexClientVersionCache

func GetLatestCodexClientVersion(ctx context.Context, client *http.Client) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lookupCtx, cancel := context.WithTimeout(ctx, codexClientVersionLookupLimit)
	defer cancel()
	return getLatestCodexClientVersionWithLookupContext(
		ctx,
		lookupCtx,
		client,
		codexLatestReleaseURL,
		time.Now(),
	)
}

func getLatestCodexClientVersionWithFallback(
	ctx context.Context,
	client *http.Client,
	releaseURL string,
	now time.Time,
) (string, error) {
	return getLatestCodexClientVersionWithLookupContext(ctx, ctx, client, releaseURL, now)
}

func getLatestCodexClientVersionWithLookupContext(
	parentCtx context.Context,
	lookupCtx context.Context,
	client *http.Client,
	releaseURL string,
	now time.Time,
) (string, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	if lookupCtx == nil {
		lookupCtx = parentCtx
	}
	version, err := latestCodexClientVersion.get(lookupCtx, client, releaseURL, now)
	if err == nil {
		return version, nil
	}
	if parentCtx.Err() != nil {
		return "", err
	}
	fallback := strings.TrimSpace(os.Getenv("CODEX_CLIENT_VERSION"))
	if fallback == "" {
		fallback = codexClientVersionFallback
	}
	latestCodexClientVersion.Lock()
	if latestCodexClientVersion.version == "" {
		latestCodexClientVersion.version = fallback
		latestCodexClientVersion.expiresAt = now.Add(codexClientVersionCacheTTL)
	}
	version = latestCodexClientVersion.version
	latestCodexClientVersion.Unlock()
	return version, nil
}

func (cache *codexClientVersionCache) get(ctx context.Context, client *http.Client, releaseURL string, now time.Time) (string, error) {
	cache.Lock()
	if cache.version != "" && now.Before(cache.expiresAt) {
		version := cache.version
		cache.Unlock()
		return version, nil
	}
	cache.Unlock()

	version, err := fetchLatestCodexClientVersion(ctx, client, releaseURL)
	cache.Lock()
	defer cache.Unlock()

	// Another caller may have completed the lookup while this request was in
	// flight. Prefer that newer value over an older failed/successful result.
	if cache.version != "" && now.Before(cache.expiresAt) {
		return cache.version, nil
	}
	if err != nil {
		if cache.version != "" {
			cache.expiresAt = now.Add(codexClientVersionCacheTTL)
			return cache.version, nil
		}
		return "", err
	}

	cache.version = version
	cache.expiresAt = now.Add(codexClientVersionCacheTTL)
	return version, nil
}

func fetchLatestCodexClientVersion(ctx context.Context, client *http.Client, releaseURL string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("nil http client")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "new-api")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("codex release lookup failed: status=%d", resp.StatusCode)
	}

	var release struct {
		Name       string `json:"name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCodexReleaseResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxCodexReleaseResponseBytes {
		return "", fmt.Errorf("response body exceeds %d bytes", maxCodexReleaseResponseBytes)
	}
	if err := common.Unmarshal(body, &release); err != nil {
		return "", err
	}
	if release.Draft || release.Prerelease {
		return "", fmt.Errorf("latest codex release is not stable")
	}
	version := strings.TrimSpace(release.Name)
	if version == "" {
		return "", fmt.Errorf("latest codex release has no version name")
	}
	return version, nil
}

func FetchCodexModels(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	oauthKey *CodexOAuthKey,
	clientVersion string,
) (statusCode int, models []string, err error) {
	return FetchCodexModelsWithHeaders(
		ctx,
		client,
		baseURL,
		oauthKey,
		clientVersion,
		nil,
	)
}

func FetchCodexModelsWithHeaders(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	oauthKey *CodexOAuthKey,
	clientVersion string,
	headers http.Header,
) (statusCode int, models []string, err error) {
	if client == nil {
		return 0, nil, fmt.Errorf("nil http client")
	}
	if oauthKey == nil {
		return 0, nil, fmt.Errorf("nil oauth key")
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	accessToken := strings.TrimSpace(oauthKey.AccessToken)
	accountID := strings.TrimSpace(oauthKey.AccountID)
	clientVersion = strings.TrimSpace(clientVersion)
	if baseURL == "" {
		return 0, nil, fmt.Errorf("empty baseURL")
	}
	if accessToken == "" {
		return 0, nil, fmt.Errorf("codex channel: access_token is required")
	}
	if accountID == "" {
		return 0, nil, fmt.Errorf("codex channel: account_id is required")
	}
	if clientVersion == "" {
		return 0, nil, fmt.Errorf("codex channel: client_version is required")
	}

	modelsURL, err := url.Parse(baseURL + "/backend-api/codex/models")
	if err != nil {
		return 0, nil, err
	}
	query := modelsURL.Query()
	query.Set("client_version", clientVersion)
	modelsURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL.String(), nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("ChatGPT-Account-Id", accountID)
	req.Header.Set("User-Agent", "codex-cli/"+clientVersion)
	req.Header.Set("Accept", "application/json")
	applyCodexModelFetchHeaders(req, headers)

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp.StatusCode, nil, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCodexModelsResponseBytes+1))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if len(body) > maxCodexModelsResponseBytes {
		return resp.StatusCode, nil, fmt.Errorf("response body exceeds %d bytes", maxCodexModelsResponseBytes)
	}

	var result struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := common.Unmarshal(body, &result); err != nil {
		return resp.StatusCode, nil, err
	}

	seen := make(map[string]struct{}, len(result.Models))
	models = make([]string, 0, len(result.Models))
	for _, item := range result.Models {
		slug := strings.TrimSpace(item.Slug)
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		models = append(models, slug)
	}
	return resp.StatusCode, models, nil
}

func applyCodexModelFetchHeaders(request *http.Request, headers http.Header) {
	for name, values := range headers {
		if strings.EqualFold(name, "Host") {
			if len(values) > 0 {
				request.Host = values[len(values)-1]
			}
			continue
		}
		if len(values) == 0 {
			continue
		}
		request.Header.Set(name, values[0])
		for _, value := range values[1:] {
			request.Header.Add(name, value)
		}
	}
}
