package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

type CodexChannelModelFetchOptions struct {
	AllowCredentialRefresh bool
}

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
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	clientVersion, err := GetLatestCodexClientVersion(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to get Codex client version: %w", err)
	}

	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeCodex]
	}
	return fetchCodexChannelModels(ctx, channel, baseURL, client, clientVersion, options.AllowCredentialRefresh)
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
	oauthKey, err := parseCodexOAuthKey(strings.TrimSpace(channel.Key))
	if err != nil {
		return nil, err
	}

	statusCode, models, err := FetchCodexModels(ctx, client, baseURL, oauthKey, clientVersion)
	if err != nil {
		return nil, err
	}
	if statusCode == http.StatusUnauthorized {
		if !allowCredentialRefresh {
			return nil, fmt.Errorf("codex channel draft credential cannot be refreshed before save")
		}
		if channel.Id <= 0 {
			return nil, fmt.Errorf("codex channel credential expired; save the channel before retrying model fetch")
		}
		refreshedKey, refreshedChannel, refreshErr := RefreshCodexChannelCredential(
			ctx,
			channel.Id,
			CodexCredentialRefreshOptions{ResetCaches: true},
		)
		if refreshErr != nil {
			return nil, fmt.Errorf("failed to refresh Codex channel credential: %w", refreshErr)
		}
		if refreshedChannel != nil {
			channel.Key = refreshedChannel.Key
		}
		statusCode, models, err = FetchCodexModels(ctx, client, baseURL, &CodexOAuthKey{
			AccessToken: refreshedKey.AccessToken,
			AccountID:   refreshedKey.AccountID,
		}, clientVersion)
		if err != nil {
			return nil, err
		}
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("upstream status: %d", statusCode)
	}
	return models, nil
}
