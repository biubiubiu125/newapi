package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

type CodexChannelModelFetchOptions struct {
	AllowCredentialRefresh bool
	Headers                http.Header
	BuildHeaders           func(channel *model.Channel, key string) (http.Header, error)
}

// CodexChannelModelFetchError indicates that the provider returned models, but
// the runtime channel cache could not be refreshed after a credential change.
// Callers can retry the cache refresh and continue with Models when it
// succeeds, instead of dropping an otherwise valid provider response.
type CodexChannelModelFetchError struct {
	Models []string
	Err    error
}

func (e *CodexChannelModelFetchError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return "models fetched but runtime channel cache refresh failed"
	}
	return fmt.Sprintf("models fetched but runtime channel cache refresh failed: %v", e.Err)
}

func (e *CodexChannelModelFetchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

var refreshCodexChannelCredentialForModelFetch = RefreshCodexChannelCredential
var refreshCodexRuntimeChannelCacheForModelFetch = refreshCodexRuntimeChannelCache

func FetchCodexChannelModels(ctx context.Context, channel *model.Channel) ([]string, error) {
	return FetchCodexChannelModelsWithOptions(ctx, channel, CodexChannelModelFetchOptions{
		AllowCredentialRefresh: true,
	})
}

func FetchCodexChannelModelsWithOptions(
	ctx context.Context,
	channel *model.Channel,
	options CodexChannelModelFetchOptions,
) ([]string, error) {
	if channel == nil || channel.Type != constant.ChannelTypeCodex {
		return nil, fmt.Errorf("channel type is not Codex")
	}
	if channel.ChannelInfo.IsMultiKey {
		return nil, fmt.Errorf("codex channel does not support multi-key model discovery")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	client, err := newCodexChannelHTTPClient(channel)
	if err != nil {
		return nil, err
	}

	clientVersion, err := GetLatestCodexClientVersion(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to get Codex client version: %w", err)
	}

	providerCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeCodex]
	}
	originalKey := channel.Key
	models, fetchErr := fetchCodexChannelModelsWithHeaders(
		providerCtx,
		channel,
		baseURL,
		client,
		clientVersion,
		options.AllowCredentialRefresh,
		options.Headers,
		options.BuildHeaders,
	)
	if channel.Key != originalKey {
		if cacheErr := refreshCodexRuntimeChannelCacheForModelFetch(); cacheErr != nil {
			if fetchErr == nil {
				return models, &CodexChannelModelFetchError{
					Models: models,
					Err:    cacheErr,
				}
			}
			return nil, fmt.Errorf("%w; runtime channel cache refresh failed: %v", fetchErr, cacheErr)
		}
	}
	return models, fetchErr
}

func newCodexChannelHTTPClient(channel *model.Channel) (*http.Client, error) {
	if channel == nil {
		return nil, fmt.Errorf("channel is nil")
	}
	settings := channel.GetSetting()
	client, err := GetHttpClientWithProxySettings(settings.Proxy, settings)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("codex channel http client is nil")
	}
	return CloneHTTPClientWithoutRedirects(client), nil
}

func fetchCodexChannelModels(
	ctx context.Context,
	channel *model.Channel,
	baseURL string,
	client *http.Client,
	clientVersion string,
	allowCredentialRefresh bool,
) ([]string, error) {
	return fetchCodexChannelModelsWithHeaders(
		ctx,
		channel,
		baseURL,
		client,
		clientVersion,
		allowCredentialRefresh,
		nil,
		nil,
	)
}

func fetchCodexChannelModelsWithHeaders(
	ctx context.Context,
	channel *model.Channel,
	baseURL string,
	client *http.Client,
	clientVersion string,
	allowCredentialRefresh bool,
	headers http.Header,
	buildHeaders func(channel *model.Channel, key string) (http.Header, error),
) ([]string, error) {
	oauthKey, err := parseCodexOAuthKey(strings.TrimSpace(channel.Key))
	if err != nil {
		return nil, err
	}

	statusCode, models, err := FetchCodexModelsWithHeaders(
		ctx,
		client,
		baseURL,
		oauthKey,
		clientVersion,
		headers,
	)
	if err != nil {
		return nil, err
	}
	if shouldRefreshCodexChannelModelCredential(statusCode) {
		if !allowCredentialRefresh {
			return nil, fmt.Errorf("codex channel draft credential cannot be refreshed before save")
		}
		if channel.Id <= 0 {
			return nil, fmt.Errorf("codex channel credential expired; save the channel before retrying model fetch")
		}
		refreshedKey, refreshedChannel, refreshErr := refreshCodexChannelCredentialForModelFetch(
			ctx,
			channel.Id,
			CodexCredentialRefreshOptions{ResetCaches: true},
		)
		if refreshErr != nil && !errors.Is(refreshErr, ErrCodexCredentialCacheRefresh) {
			return nil, fmt.Errorf("failed to refresh Codex channel credential: %w", refreshErr)
		}
		if refreshedKey == nil {
			return nil, fmt.Errorf("failed to refresh Codex channel credential: refreshed key is empty")
		}
		if refreshedChannel != nil {
			channel.Key = refreshedChannel.Key
		}
		retryHeaders := headers
		if buildHeaders != nil {
			retryHeaders, err = buildHeaders(channel, refreshedKey.AccessToken)
			if err != nil {
				return nil, fmt.Errorf("failed to rebuild Codex model request headers: %w", err)
			}
		}
		statusCode, models, err = FetchCodexModelsWithHeaders(
			ctx,
			client,
			baseURL,
			&CodexOAuthKey{
				AccessToken: refreshedKey.AccessToken,
				AccountID:   refreshedKey.AccountID,
			},
			clientVersion,
			retryHeaders,
		)
		if err != nil {
			return nil, err
		}
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("upstream status: %d", statusCode)
	}
	return models, nil
}

func shouldRefreshCodexChannelModelCredential(statusCode int) bool {
	return statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden
}
