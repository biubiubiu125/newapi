package service

import (
	"errors"
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The configured DSNs must point at isolated test databases. Each dialect runs
// the real reservation, settlement and log paths with the same billing cases.
func TestFixedPriceBillingDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct {
		name   common.DatabaseType
		env    string
		logEnv string
	}{
		{common.DatabaseTypeSQLite, "", ""},
		{common.DatabaseTypeMySQL, "TEST_FIXED_MYSQL_DSN", "TEST_FIXED_MYSQL_LOG_DSN"},
		{common.DatabaseTypePostgreSQL, "TEST_FIXED_POSTGRES_DSN", "TEST_FIXED_POSTGRES_LOG_DSN"},
	} {
		t.Run(string(dialect.name), func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			if dialect.env != "" {
				dsn := os.Getenv(dialect.env)
				if dsn == "" {
					t.Skip(dialect.env + " is not configured")
				}
				if dialect.name == common.DatabaseTypeMySQL {
					driver = mysql.Open(dsn)
				} else {
					driver = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			logDB := db
			if logDSN := os.Getenv(dialect.logEnv); logDSN != "" || dialect.name == common.DatabaseTypeSQLite {
				var logDriver gorm.Dialector = sqlite.Open(":memory:")
				if dialect.name == common.DatabaseTypeMySQL {
					logDriver = mysql.Open(logDSN)
				} else if dialect.name == common.DatabaseTypePostgreSQL {
					logDriver = postgres.New(postgres.Config{DSN: logDSN, PreferSimpleProtocol: true})
				}
				logDB, err = gorm.Open(logDriver, &gorm.Config{})
				require.NoError(t, err)
				logSQL, err := logDB.DB()
				require.NoError(t, err)
				logSQL.SetMaxOpenConns(1)
				t.Cleanup(func() { require.NoError(t, logSQL.Close()) })
			}
			oldDB, oldLogDB := model.DB, model.LOG_DB
			oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			model.DB, model.LOG_DB = db, logDB
			common.SetDatabaseTypes(dialect.name, dialect.name)
			t.Cleanup(func() { model.DB, model.LOG_DB = oldDB, oldLogDB; common.SetDatabaseTypes(oldMainType, oldLogType) })
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}))
			require.NoError(t, logDB.AutoMigrate(&model.Log{}))
			versionQuery := "select version()"
			if dialect.name == common.DatabaseTypeSQLite {
				versionQuery = "select sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database: %s", version)
			runFixedPriceAccountingCases(t, db, logDB)
		})
	}
}

func runFixedPriceAccountingCases(t *testing.T, db, logDB *gorm.DB) {
	t.Helper()
	const mixed = `len <= 32000 ? tier("short", fixed(0.01)) : tier("long", p * 2)`
	const flat = `tier("request", fixed(0.01))`
	const startingQuota = 2_000_000
	const imageExpression = `tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`
	imageUsage := &dto.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100,
		PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 300, ImageTokens: 600, CachedTokensDetails: &dto.CachedTokenDetails{ImageTokens: common.GetPointer(200)}}}
	operation_setting.SetToolPriceForTest("fixed_billing_tool", 4)
	t.Cleanup(func() { operation_setting.DeleteToolPriceForTest("fixed_billing_tool") })
	for index, tc := range []struct {
		name, expression                          string
		estimate                                  int
		usage                                     *dto.Usage
		audio, stream, refund, insufficient, tool bool
		realtime, reserveInsufficient             bool
		wallet, outboundImages                    int
		groupRatio                                float64
		want                                      int
		unit                                      billingexpr.BillingUnit
		requestedImages, actualImages             int
	}{
		{name: "missing usage charges once", expression: flat, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "zero usage charges once", expression: flat, usage: &dto.Usage{}, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "stream charges once", expression: flat, stream: true, usage: &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "audio zero usage charges once", expression: flat, audio: true, usage: &dto.Usage{}, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "audio missing usage charges once", expression: flat, audio: true, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "token reservation refunds to fixed price", expression: mixed, estimate: 50000, usage: &dto.Usage{PromptTokens: 100, TotalTokens: 100}, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "fixed reservation settles token fallback", expression: mixed, estimate: 100, usage: &dto.Usage{PromptTokens: 50000, TotalTokens: 50000}, want: 50000, unit: billingexpr.BillingUnitToken},
		{name: "missing usage uses estimated token fallback", expression: mixed, estimate: 50000, want: 50000, unit: billingexpr.BillingUnitToken},
		{name: "evaluation error retains fixed reservation metadata", expression: `p == 50 ? tier("error", param("missing") * p + img_cr * 2) : tier("request", fixed(0.01))`, estimate: 100, usage: &dto.Usage{PromptTokens: 50, TotalTokens: 50}, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "explicit zero remains free", expression: `tier("free", fixed(0))`, usage: &dto.Usage{PromptTokens: 100, TotalTokens: 100}, unit: billingexpr.BillingUnitRequest},
		{name: "multipliers and separate tool surcharge", expression: flat + ` * (param("fast") == true ? 2 : 1)`, groupRatio: 1.5, tool: true, want: 18000, unit: billingexpr.BillingUnitRequest},
		{name: "failed request refunds exactly once", expression: flat, refund: true},
		{name: "insufficient wallet never reserves tokens", expression: flat, insufficient: true},
		{name: "image cache stream settles usage and refunds unused reservation", expression: imageExpression, estimate: 10000, usage: imageUsage, stream: true, want: 4113, unit: billingexpr.BillingUnitToken},
		{name: "audio settlement records image cache billing inputs", expression: imageExpression, estimate: 10000, usage: imageUsage, audio: true, want: 4113, unit: billingexpr.BillingUnitToken},
		{name: "realtime records actual expression inputs", expression: imageExpression, estimate: 10000, usage: imageUsage, realtime: true, want: 4000, unit: billingexpr.BillingUnitToken},
		{name: "image cache insufficient wallet never reserves tokens", expression: imageExpression, estimate: 10000, insufficient: true},
		{name: "image quantity refunds missing images", expression: `tier("image", fixed(0.04)) * image_count`, requestedImages: 3, actualImages: 2, want: 40000, unit: billingexpr.BillingUnitRequest},
		{name: "image quantity keeps request when actual missing", expression: `tier("image", fixed(0.04)) * image_count`, requestedImages: 3, want: 60000, unit: billingexpr.BillingUnitRequest},
		{name: "image quantity zero price stays free", expression: `tier("image", fixed(0)) * image_count`, requestedImages: 3, actualImages: 2, unit: billingexpr.BillingUnitRequest},
		{name: "image quantity failure refunds reservation", expression: `tier("image", fixed(0.04)) * image_count`, requestedImages: 3, refund: true},
		{name: "image quantity exceeds one-image wallet before submission", expression: `tier("image", fixed(0.04)) * image_count`, requestedImages: 4, wallet: 20000, insufficient: true},
		{name: "image override reserves extra quantity", expression: `tier("image", fixed(0.04)) * image_count`, requestedImages: 1, outboundImages: 4, want: 80000, unit: billingexpr.BillingUnitRequest},
		{name: "image override cannot exceed remaining wallet", expression: `tier("image", fixed(0.04)) * image_count`, requestedImages: 1, outboundImages: 4, wallet: 40000, reserveInsufficient: true, refund: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quota := startingQuota
			if tc.insufficient {
				quota = 1
			}
			if tc.wallet > 0 {
				quota = tc.wallet
			}
			user := model.User{Username: fmt.Sprintf("fixed_billing_%d", index), Quota: int64(quota), Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			token := model.Token{UserId: user.Id, Key: fmt.Sprintf("fixed-billing-test-%d", index), Name: "fixed-billing", RemainQuota: startingQuota, Status: common.TokenStatusEnabled}
			require.NoError(t, db.Create(&token).Error)
			channel := model.Channel{Name: "fixed-billing", Key: "unused", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			t.Cleanup(func() {
				require.NoError(t, logDB.Where("user_id = ?", user.Id).Delete(&model.Log{}).Error)
				require.NoError(t, db.Unscoped().Delete(&token).Error)
				require.NoError(t, db.Unscoped().Delete(&user).Error)
				require.NoError(t, db.Unscoped().Delete(&channel).Error)
			})
			group := tc.groupRatio
			if group == 0 {
				group = 1
			}
			request := &billingexpr.RequestInput{Body: []byte(`{"fast":true}`)}
			if tc.requestedImages > 0 {
				request.ImageCount = &tc.requestedImages
			}
			cost, trace, err := billingexpr.RunExprWithRequest(tc.expression, billingexpr.TokenParams{P: float64(tc.estimate), Len: float64(tc.estimate)}, *request)
			require.NoError(t, err)
			reservation, err := billingexpr.QuotaRoundStrict(cost / 1_000_000 * common.QuotaPerUnit * group)
			require.NoError(t, err)
			snapshot := &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: tc.expression, ExprHash: billingexpr.ExprHashString(tc.expression), QuotaPerUnit: common.QuotaPerUnit, GroupRatio: group, EstimatedTier: trace.MatchedTier, EstimatedBillingUnit: trace.BillingUnit, EstimatedFixedPrice: trace.FixedPrice, EstimatedQuotaAfterGroup: reservation}
			snapshot.EstimatedImageCount = trace.ImageCount
			info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channel.Id}, OriginModelName: "fixed-test", UsingGroup: "default", UserGroup: "default", UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, ForcePreConsume: true, StartTime: time.Now(), IsStream: tc.stream, RelayFormat: types.RelayFormatOpenAI, PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: group}}, TieredBillingSnapshot: snapshot, BillingRequestInput: request}
			info.SetEstimatePromptTokens(tc.estimate)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			if tc.expression == imageExpression {
				ctx.Request.URL.Path = "/v1/images/generations"
				info.RelayMode = relayconstant.RelayModeImagesGenerations
			}
			apiErr := PreConsumeBilling(ctx, reservation, info)
			if tc.insufficient {
				require.NotNil(t, apiErr)
				assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
			} else {
				require.Nil(t, apiErr)
				held, err := model.GetUserQuota(user.Id, true)
				require.NoError(t, err)
				assert.EqualValues(t, quota-reservation, held)
				if tc.outboundImages > 0 {
					reserveErr := PrepareImageBillingForRequest(ctx, info, tc.outboundImages)
					if tc.reserveInsufficient {
						require.NotNil(t, reserveErr)
						assert.Equal(t, types.ErrorCodeInsufficientUserQuota, reserveErr.GetErrorCode())
						assert.Equal(t, reservation, info.Billing.GetPreConsumedQuota())
					} else {
						require.Nil(t, reserveErr)
						assert.Equal(t, tc.want, info.Billing.GetPreConsumedQuota())
					}
				}
				if tc.refund {
					refunded := make(chan struct{}, 1)
					const callback = "fixed_billing_refund_observed"
					require.NoError(t, db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register(callback, func(tx *gorm.DB) {
						if tx.Statement.Table == "tokens" && tx.Error == nil {
							select {
							case refunded <- struct{}{}:
							default:
							}
						}
					}))
					t.Cleanup(func() { require.NoError(t, db.Callback().Update().Remove(callback)) })
					info.Billing.Refund(ctx)
					info.Billing.Refund(ctx)
					select {
					case <-refunded:
					case <-time.After(5 * time.Second):
						t.Fatal("refund did not finish")
					}
				} else {
					info.UpdateImageCount(int64(tc.actualImages))
					if tc.tool {
						info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{"fixed_billing_tool": {CallCount: 1}}}
					}
					if tc.realtime {
						PostWssConsumeQuota(ctx, info, info.OriginModelName, &dto.RealtimeUsage{
							InputTokens: tc.usage.PromptTokens, OutputTokens: tc.usage.CompletionTokens, TotalTokens: tc.usage.TotalTokens,
						}, "")
					} else if tc.audio {
						PostAudioConsumeQuota(ctx, info, tc.usage, "")
					} else {
						PostTextConsumeQuota(ctx, info, tc.usage, nil)
					}
					require.NoError(t, info.Billing.Settle(tc.want), "a repeated settlement must not charge again")
					var log model.Log
					require.NoError(t, logDB.Where("user_id = ?", user.Id).Take(&log).Error)
					assert.Equal(t, tc.want, log.Quota)
					assert.Equal(t, tc.stream, log.IsStream)
					assert.NotContains(t, log.Content, "无法扣费")
					var other map[string]any
					require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
					assert.Equal(t, string(tc.unit), other["billing_unit"])
					if tc.requestedImages > 0 {
						count := tc.actualImages
						if count == 0 {
							count = tc.requestedImages
							if tc.outboundImages > 0 {
								count = tc.outboundImages
							}
						}
						assert.Equal(t, float64(count), other["image_count"])
						assert.Equal(t, tc.requestedImages, *request.ImageCount, "actual count must not mutate the frozen request")
					}
					if tc.expression == imageExpression {
						billable, ok := other["billing_tokens"].(map[string]any)
						require.True(t, ok)
						if tc.realtime {
							assert.Equal(t, float64(0), other["image_cache_tokens"])
							assert.Equal(t, float64(1000), billable["p"])
							assert.Equal(t, float64(0), billable["cr"])
							assert.Equal(t, float64(0), billable["img"])
						} else {
							if !tc.audio {
								assert.Equal(t, float64(300), other["cache_tokens"])
							}
							assert.Equal(t, float64(200), other["image_cache_tokens"])
							assert.Equal(t, float64(300), billable["p"])
							assert.Equal(t, float64(100), billable["cr"])
							assert.Equal(t, float64(400), billable["img"])
						}
					} else {
						assert.NotContains(t, other, "billing_tokens")
						assert.NotContains(t, other, "image_cache_tokens")
					}
					if tc.unit == billingexpr.BillingUnitRequest {
						assert.Contains(t, other, "fixed_price")
					} else {
						assert.NotContains(t, other, "fixed_price")
					}
				}
			}
			require.NoError(t, db.First(&user, user.Id).Error)
			require.NoError(t, db.First(&token, token.Id).Error)
			assert.EqualValues(t, quota-tc.want, user.Quota)
			assert.EqualValues(t, startingQuota-tc.want, token.RemainQuota)
			assert.EqualValues(t, tc.want, user.UsedQuota)
			assert.EqualValues(t, tc.want, token.UsedQuota)
			if !tc.refund && !tc.insufficient {
				assert.EqualValues(t, 1, user.RequestCount)
			}
		})
	}
}

func TestCalculateTextQuotaSummaryUnifiedForClaudeSemantic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         100,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
	}

	priceData := types.PriceData{
		ModelRatio:           1,
		CompletionRatio:      2,
		CacheRatio:           0.1,
		CacheCreationRatio:   1.25,
		CacheCreation5mRatio: 1.25,
		CacheCreation1hRatio: 2,
		GroupRatioInfo: types.GroupRatioInfo{
			GroupRatio: 1,
		},
	}

	chatRelayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData:               priceData,
		StartTime:               time.Now(),
	}
	messageRelayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatClaude,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData:               priceData,
		StartTime:               time.Now(),
	}

	chatSummary := calculateTextQuotaSummary(ctx, chatRelayInfo, usage)
	messageSummary := calculateTextQuotaSummary(ctx, messageRelayInfo, usage)

	require.EqualValues(t, messageSummary.Quota, chatSummary.Quota)
	require.Equal(t, messageSummary.CacheCreationTokens5m, chatSummary.CacheCreationTokens5m)
	require.Equal(t, messageSummary.CacheCreationTokens1h, chatSummary.CacheCreationTokens1h)
	require.True(t, chatSummary.IsClaudeUsageSemantic)
	require.EqualValues(t, 1488, chatSummary.Quota)
}

func TestCalculateTextQuotaSummaryUsesSplitClaudeCacheCreationRatios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      1,
			CacheRatio:           0,
			CacheCreationRatio:   1,
			CacheCreation5mRatio: 2,
			CacheCreation1hRatio: 3,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 0,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedCreationTokens: 10,
		},
		ClaudeCacheCreation5mTokens: 2,
		ClaudeCacheCreation1hTokens: 3,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 100 + remaining(5)*1 + 2*2 + 3*3 = 118
	require.EqualValues(t, 118, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesAnthropicUsageSemanticFromUpstreamUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      2,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		UsageSemantic:    "anthropic",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         100,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.True(t, summary.IsClaudeUsageSemantic)
	require.EqualValues(t, "anthropic", summary.UsageSemantic)
	require.EqualValues(t, 1488, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesClaudeBillingUsageBeforeTopLevelUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      2,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo:       types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     999,
		CompletionTokens: 999,
		TotalTokens:      1998,
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{
			InputTokens:              70,
			CacheReadInputTokens:     30,
			CacheCreationInputTokens: 20,
			OutputTokens:             7,
			CacheCreation: &dto.ClaudeCacheCreationUsage{
				Ephemeral5mInputTokens: 12,
				Ephemeral1hInputTokens: 8,
			},
		}),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, effectiveBillingUsage(usage))

	require.True(t, summary.IsClaudeUsageSemantic)
	require.EqualValues(t, dto.BillingUsageSemanticAnthropic, summary.UsageSemantic)
	require.Equal(t, 70, summary.PromptTokens)
	require.Equal(t, 7, summary.CompletionTokens)
	require.Equal(t, 30, summary.CacheTokens)
	require.Equal(t, 20, summary.CacheCreationTokens)
	require.Equal(t, 12, summary.CacheCreationTokens5m)
	require.Equal(t, 8, summary.CacheCreationTokens1h)
	require.EqualValues(t, 118, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesGeminiBillingUsageBeforeTopLevelUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gemini-2.5-flash",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			CacheRatio:      0.1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     999,
		CompletionTokens: 999,
		TotalTokens:      1998,
		BillingUsage: dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{
			PromptTokenCount:        100,
			ToolUsePromptTokenCount: 5,
			CandidatesTokenCount:    20,
			ThoughtsTokenCount:      3,
			TotalTokenCount:         128,
			CachedContentTokenCount: 7,
		}),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, effectiveBillingUsage(usage))

	require.False(t, summary.IsClaudeUsageSemantic)
	require.EqualValues(t, dto.BillingUsageSemanticGemini, summary.UsageSemantic)
	require.Equal(t, 105, summary.PromptTokens)
	require.Equal(t, 23, summary.CompletionTokens)
	require.Equal(t, 7, summary.CacheTokens)
	require.Equal(t, 128, summary.TotalTokens)
	require.EqualValues(t, 145, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesOpenAIBillingUsageBeforeTopLevelUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "gpt-4o",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     999,
		CompletionTokens: 999,
		TotalTokens:      1998,
		BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{
			PromptTokens:     80,
			CompletionTokens: 9,
			TotalTokens:      89,
		}),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, effectiveBillingUsage(usage))

	require.False(t, summary.IsClaudeUsageSemantic)
	require.EqualValues(t, dto.BillingUsageSemanticOpenAI, summary.UsageSemantic)
	require.Equal(t, 80, summary.PromptTokens)
	require.Equal(t, 9, summary.CompletionTokens)
	require.Equal(t, 89, summary.TotalTokens)
	require.EqualValues(t, 98, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesOpenAIResponsesInputTokenDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-4o",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 2,
			CacheRatio:      0.25,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	responsesUsage := &dto.Usage{
		InputTokens:  100,
		OutputTokens: 10,
		TotalTokens:  110,
		InputTokensDetails: &dto.InputTokenDetails{
			CachedTokens: 40,
		},
	}
	convertedUsage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 10,
		TotalTokens:      110,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 40,
		},
		BillingUsage: dto.NewOpenAIResponsesBillingUsage(responsesUsage),
	}

	effectiveUsage := effectiveBillingUsage(convertedUsage)
	require.EqualValues(t, 40, effectiveUsage.PromptTokensDetails.CachedTokens)
	require.Zero(t, convertedUsage.BillingUsage.OpenAIUsage.PromptTokensDetails.CachedTokens)

	summary := calculateTextQuotaSummary(ctx, relayInfo, effectiveUsage)
	require.Equal(t, 40, summary.CacheTokens)
	// 60 uncached input + 40*0.25 cached input + 10*2 output = 90.
	require.EqualValues(t, 90, summary.Quota)
}

func TestUsageFromOpenAIBillingUsageNormalizesCacheDetailsWithoutOverwritingCanonicalValues(t *testing.T) {
	responsesUsage := &dto.Usage{
		InputTokens:          100,
		OutputTokens:         10,
		PromptCacheHitTokens: 55,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 8,
			TextTokens:   12,
		},
		InputTokensDetails: &dto.InputTokenDetails{
			CachedTokens:         40,
			CachedCreationTokens: 5,
			CacheWriteTokens:     6,
			TextTokens:           60,
			ImageTokens:          7,
			AudioTokens:          9,
		},
	}

	billingUsage := dto.NewOpenAIResponsesBillingUsage(responsesUsage)
	usage := effectiveBillingUsage(&dto.Usage{BillingUsage: billingUsage})

	require.EqualValues(t, 8, usage.PromptTokensDetails.CachedTokens)
	require.EqualValues(t, 5, usage.PromptTokensDetails.CachedCreationTokens)
	require.EqualValues(t, 6, usage.PromptTokensDetails.CacheWriteTokens)
	require.EqualValues(t, 12, usage.PromptTokensDetails.TextTokens)
	require.EqualValues(t, 7, usage.PromptTokensDetails.ImageTokens)
	require.EqualValues(t, 9, usage.PromptTokensDetails.AudioTokens)
	require.Zero(t, billingUsage.OpenAIUsage.PromptTokensDetails.CachedCreationTokens)
}

func TestUsageFromOpenAIBillingUsageFallsBackToPromptCacheHitTokens(t *testing.T) {
	usage := effectiveBillingUsage(&dto.Usage{
		BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{
			PromptTokens:         100,
			CompletionTokens:     10,
			PromptCacheHitTokens: 35,
		}),
	})

	require.EqualValues(t, 35, usage.PromptTokensDetails.CachedTokens)
}

func TestCalculateTextQuotaSummaryNormalizesOpenAIResponsesBillingUsageDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "gpt-5.6-sol",
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    2,
			CacheRatio:         0.5,
			CacheCreationRatio: 2,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	responsesDetails := dto.InputTokenDetails{
		CachedTokens:     80,
		CacheWriteTokens: 10,
		TextTokens:       100,
	}
	usage := &dto.Usage{
		PromptTokens:     999,
		CompletionTokens: 999,
		BillingUsage: dto.NewOpenAIResponsesBillingUsage(&dto.Usage{
			InputTokens:        100,
			OutputTokens:       10,
			TotalTokens:        110,
			InputTokensDetails: &responsesDetails,
		}),
	}

	effectiveUsage := effectiveBillingUsage(usage)
	summary := calculateTextQuotaSummary(ctx, relayInfo, effectiveUsage)

	require.Equal(t, dto.BillingUsageSourceOAIResponses, effectiveUsage.UsageSource)
	require.Equal(t, responsesDetails, effectiveUsage.PromptTokensDetails)
	require.Equal(t, 100, summary.PromptTokens)
	require.Equal(t, 10, summary.CompletionTokens)
	require.Equal(t, 80, summary.CacheTokens)
	require.Equal(t, 10, summary.CacheCreationTokens)
	// (100-80-10) + 80*0.5 + 10*2 + 10*2 = 90
	require.Equal(t, 90, summary.Quota)
}

func TestUsageBillingPathForLog(t *testing.T) {
	require.EqualValues(t, usageBillingPathAnthropic, usageBillingPathForLog(true, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	}))
	require.EqualValues(t, usageBillingPathLocal, usageBillingPathForLog(true, &dto.Usage{}))
	require.EqualValues(t, usageBillingPathUpstream, usageBillingPathForLog(false, &dto.Usage{}))
	require.EqualValues(t, usageBillingPathOpenAI, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{PromptTokens: 1}),
	}))
	require.EqualValues(t, usageBillingPathAnthropic, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	}))
	require.EqualValues(t, usageBillingPathGemini, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{PromptTokenCount: 1}),
	}))
	require.EqualValues(t, usageBillingPathGeminiEstimated, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewEstimatedGeminiChatBillingUsage(&dto.Usage{PromptTokens: 1}),
	}))
}

func TestAppendUsageBillingPathForLogWritesAdminInfo(t *testing.T) {
	other := model.NewLogOther()
	appendUsageBillingPathForLog(other, false, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	})

	adminInfo, ok := other.Snapshot()["admin_info"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, usageBillingPathAnthropic, adminInfo["usage_billing_path"])

	other = model.NewLogOther()
	appendUsageBillingPathForLog(other, true, nil)
	adminInfo, ok = other.Snapshot()["admin_info"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, usageBillingPathLocal, adminInfo["usage_billing_path"])
}

func TestCacheWriteTokensTotal(t *testing.T) {
	t.Run("split cache creation", func(t *testing.T) {
		summary := textQuotaSummary{
			CacheCreationTokens:   50,
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 50, cacheWriteTokensTotal(summary))
	})

	t.Run("legacy cache creation", func(t *testing.T) {
		summary := textQuotaSummary{CacheCreationTokens: 50}
		require.Equal(t, 50, cacheWriteTokensTotal(summary))
	})

	t.Run("split cache creation without aggregate remainder", func(t *testing.T) {
		summary := textQuotaSummary{
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 30, cacheWriteTokensTotal(summary))
	})
}

func TestCalculateTextQuotaSummaryHandlesLegacyClaudeDerivedOpenAIUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      5,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo:       types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     62,
		CompletionTokens: 95,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 3544,
		},
		ClaudeCacheCreation5mTokens: 586,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 62 + 3544*0.1 + 586*1.25 + 95*5 = 1624.9 => 1624
	require.EqualValues(t, 1624, summary.Quota)
}

func TestCalculateTextQuotaSummaryBillsOpenAICacheWriteTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-5.1",
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    2,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	t.Run("uncached remainder stays positive", func(t *testing.T) {
		usage := &dto.Usage{
			PromptTokens:     1473,
			CompletionTokens: 19,
			PromptTokensDetails: dto.InputTokenDetails{
				CacheWriteTokens: 1470,
			},
		}

		summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

		require.Equal(t, 1470, summary.CacheCreationTokens)
		// (1473-0-1470) + 1470*1.25 + 19*2 = 3 + 1837.5 + 38 = 1878.5 => 1879
		require.EqualValues(t, 1879, summary.Quota)
	})

	t.Run("uncached remainder clamps to zero", func(t *testing.T) {
		// Real OpenAI payload shape: cached_tokens + cache_write_tokens exceeds
		// prompt_tokens because both are unadjusted prefix counts. The negative
		// remainder must clamp to zero, never turn into a negative base charge.
		usage := &dto.Usage{
			PromptTokens:     3619,
			CompletionTokens: 36,
			PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens:     2921,
				CacheWriteTokens: 3616,
			},
		}

		summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

		require.Equal(t, 3619, summary.PromptTokens)
		require.Equal(t, 3616, summary.CacheCreationTokens)
		// max(3619-2921-3616, 0) + 2921*0.1 + 3616*1.25 + 36*2 = 4884.1 => 4884
		require.EqualValues(t, 4884, summary.Quota)
	})
}

func TestCalculateTextQuotaSummarySeparatesOpenRouterCacheReadFromPromptBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "openai/gpt-4.1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 2432,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// OpenRouter OpenAI-format display keeps prompt_tokens as total input,
	// but billing still separates normal input from cache read tokens.
	// quota = (2604 - 2432) + 2432*0.1 + 383 = 798.2 => 798
	require.Equal(t, 2604, summary.PromptTokens)
	require.EqualValues(t, 798, summary.Quota)
}

func TestCalculateTextQuotaSummarySeparatesOpenRouterCacheCreationFromPromptBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "openai/gpt-4.1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedCreationTokens: 100,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// prompt_tokens is still logged as total input, but cache creation is billed separately.
	// quota = (2604 - 100) + 100*1.25 + 383 = 3012
	require.Equal(t, 2604, summary.PromptTokens)
	require.EqualValues(t, 3012, summary.Quota)
}

func TestCalculateTextQuotaSummaryKeepsPrePRClaudeOpenRouterBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "anthropic/claude-3.7-sonnet",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 2432,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// Pre-PR PostClaudeConsumeQuota behavior for OpenRouter:
	// prompt = 2604 - 2432 = 172
	// quota = 172 + 2432*0.1 + 383 = 798.2 => 798
	require.True(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, 172, summary.PromptTokens)
	require.EqualValues(t, 798, summary.Quota)
}

func TestComposeTieredTextQuotaKeepsToolCallSurcharges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("image_generation_call", true)
	ctx.Set("image_generation_call_quality", "low")
	ctx.Set("image_generation_call_size", "1024x1024")

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: &relaycommon.BuildInToolInfo{
					CallCount: 1,
				},
				dto.BuildInToolFileSearch: &relaycommon.BuildInToolInfo{
					CallCount: 2,
				},
			},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1,
			EstimatedQuotaBeforeGroup: 1000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	quota := composeTieredTextQuota(relayInfo, summary, 1000, &billingexpr.TieredResult{
		ActualQuotaBeforeGroup: 1000,
		ActualQuotaAfterGroup:  1000,
	})

	require.EqualValues(t, int64(13000), summary.ToolCallSurchargeQuota.Round(0).IntPart())
	require.EqualValues(t, 14000, quota)
}

func TestComposeTieredTextQuotaFallbackKeepsToolCallSurcharges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("claude_web_search_requests", 2)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1.25},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1.25,
			EstimatedQuotaBeforeGroup: 1000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	quota := composeTieredTextQuota(relayInfo, summary, 1250, nil)

	require.EqualValues(t, int64(12500), summary.ToolCallSurchargeQuota.Round(0).IntPart())
	require.EqualValues(t, 13750, quota)
}

func TestComposeTieredTextQuotaErrorFallbackUsesPreConsumedQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("claude_web_search_requests", 2)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1.25},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1.25,
			EstimatedQuotaBeforeGroup: 1000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// tieredResult=nil simulates a settlement error where TryTieredSettle
	// falls back to FinalPreConsumedQuota (2000), which differs from
	// EstimatedQuotaBeforeGroup * GroupRatio (1250).
	preConsumedFallback := 2000
	quota := composeTieredTextQuota(relayInfo, summary, preConsumedFallback, nil)

	require.EqualValues(t, int64(12500), summary.ToolCallSurchargeQuota.Round(0).IntPart())
	require.EqualValues(t, 14500, quota)
}

func TestCalculateTextQuotaSummaryBillsNativeResponsesTools(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("gemini_google_search_call", true)
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4.1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearch:       {ToolName: dto.BuildInToolWebSearch, CallCount: 1},
				dto.BuildInToolImageGeneration: {ToolName: dto.BuildInToolImageGeneration, CallCount: 1},
			},
		},
		StartTime: time.Now(),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, &dto.Usage{})
	require.Greater(t, summary.ToolCallSurchargeQuota.InexactFloat64(), 0.0)
	require.Greater(t, summary.Quota, 0)
	require.Equal(t, 1, summary.WebSearchCallCount)
}

func TestPrepareTieredBillingForSelectedGroupReservesNonTieredIncrease(t *testing.T) {
	billing := &recordingBillingSettler{preConsumedQuota: 100}
	relayInfo := &relaycommon.RelayInfo{
		Billing:               billing,
		FinalPreConsumedQuota: 100,
		PriceData: types.PriceData{
			FreeModel:         false,
			QuotaToPreConsume: 400,
			GroupRatioInfo:    types.GroupRatioInfo{GroupRatio: 2},
		},
	}

	require.Nil(t, PrepareTieredBillingForSelectedGroup(nil, relayInfo))
	require.Equal(t, []int{400}, billing.reserveTargets)
	assert.EqualValues(t, 400, billing.preConsumedQuota)
	assert.EqualValues(t, 400, relayInfo.FinalPreConsumedQuota)
}

type failingTextQuotaSettlementFunding struct {
	err         error
	settleCalls int
}

func (f *failingTextQuotaSettlementFunding) Source() string { return BillingSourceWallet }
func (f *failingTextQuotaSettlementFunding) PreConsume(amount int) error {
	return nil
}
func (f *failingTextQuotaSettlementFunding) Settle(delta int) error {
	f.settleCalls++
	return f.err
}
func (f *failingTextQuotaSettlementFunding) Refund() error {
	return nil
}

func TestPostTextConsumeQuotaCheckedRecordsUsageLogWhenSettlementFails(t *testing.T) {
	truncate(t)
	oldDataExportEnabled := common.DataExportEnabled
	common.DataExportEnabled = true
	t.Cleanup(func() {
		common.DataExportEnabled = oldDataExportEnabled
		model.CacheQuotaDataLock.Lock()
		model.CacheQuotaData = make(map[string]*model.QuotaData)
		model.CacheQuotaDataLock.Unlock()
	})
	model.CacheQuotaDataLock.Lock()
	model.CacheQuotaData = make(map[string]*model.QuotaData)
	model.CacheQuotaDataLock.Unlock()
	require.NoError(t, model.DB.Create(&model.User{
		Id:       9501,
		Username: "settle-log-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Quota:    10000,
	}).Error)
	seedChannel(t, 1)
	seedToken(t, 2, 9501, "settle-token-key", 1000)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "settle-log-owner")
	ctx.Set("token_name", "settle-token")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          9501,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 1},
		TokenId:         2,
		TokenKey:        "settle-token-key",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_settle_1"},
	}
	funding := &failingTextQuotaSettlementFunding{err: errors.New("settlement failed")}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          funding,
		preConsumedQuota: 10,
	}
	usage := &dto.Usage{
		PromptTokens:     20,
		CompletionTokens: 10,
		TotalTokens:      30,
	}

	err := PostTextConsumeQuotaChecked(ctx, relayInfo, usage, nil)

	require.Error(t, err)
	require.Equal(t, 1, funding.settleCalls)
	var log model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", 9501, model.LogTypeConsume).First(&log).Error)
	require.Equal(t, "settle-log-owner", log.Username)
	require.Equal(t, "gpt-4o", log.ModelName)
	require.EqualValues(t, 10, log.Quota)
	require.Contains(t, log.Other, "settlement_error")
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	require.Equal(t, "task_settle_1", other["task_id"])
	require.EqualValues(t, 30, other["attempted_quota"])
	require.EqualValues(t, 10, other["settled_quota"])

	stat, err := model.SumUsedQuota(model.LogTypeConsume, 0, 0, "gpt-4o", "settle-log-owner", "settle-token", 0, "")
	require.NoError(t, err)
	require.EqualValues(t, 10, stat.Quota)

	require.Eventually(t, func() bool {
		model.CacheQuotaDataLock.Lock()
		defer model.CacheQuotaDataLock.Unlock()
		return len(model.CacheQuotaData) > 0
	}, time.Second, 10*time.Millisecond)
	model.SaveQuotaDataCache()

	var quotaData model.QuotaData
	require.NoError(t, model.DB.Where("user_id = ? AND model_name = ?", 9501, "gpt-4o").First(&quotaData).Error)
	require.EqualValues(t, "settle-log-owner", quotaData.Username)
	require.EqualValues(t, 10, quotaData.Quota)
}

func TestPostTextConsumeQuotaRecordsAccountingErrorWhenSettlementFails(t *testing.T) {
	truncate(t)
	seedUser(t, 9545, 10000)
	seedChannel(t, 9546)
	seedToken(t, 9547, 9545, "text-settlement-audit-token", 1000)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "text-settlement-audit-owner")
	ctx.Set("token_name", "text-settlement-audit-token")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          9545,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 9546},
		TokenId:         9547,
		TokenKey:        "text-settlement-audit-token",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &failingTextQuotaSettlementFunding{err: errors.New("text settlement failed")},
		preConsumedQuota: 10,
	}
	usage := &dto.Usage{
		PromptTokens:     20,
		CompletionTokens: 10,
		TotalTokens:      30,
	}

	PostTextConsumeQuota(ctx, relayInfo, usage, nil)

	var consumeLog model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", 9545, model.LogTypeConsume).First(&consumeLog).Error)
	require.Contains(t, consumeLog.Other, "settlement_error")
	var errorLog model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ? AND content LIKE ?", 9545, model.LogTypeError, "%post text consume quota settlement failed%").First(&errorLog).Error)
	require.Contains(t, errorLog.Other, "accounting_error")
}

func TestPreWssConsumeQuotaSkipsWhenPriceDataUsePrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		UsePrice: false,
		PriceData: types.PriceData{
			UsePrice:   true,
			ModelPrice: 1,
		},
	}
	err := PreWssConsumeQuota(ctx, relayInfo, &dto.RealtimeUsage{
		TotalTokens: 12,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens:  8,
			AudioTokens: 0,
		},
		OutputTokenDetails: dto.OutputTokenDetails{
			TextTokens:  4,
			AudioTokens: 0,
		},
	})
	require.NoError(t, err)
}

func TestPostWssConsumeQuotaReturnsSettlementError(t *testing.T) {
	truncate(t)
	seedUser(t, 9540, 10000)
	seedChannel(t, 9541)
	seedToken(t, 9542, 9540, "wss-settle-token", 1000)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "wss-settle-owner")
	ctx.Set("token_name", "wss-settle-token")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          9540,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 9541},
		TokenId:         9542,
		TokenKey:        "wss-settle-token",
		OriginModelName: "gpt-4o-realtime-preview",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &failingTextQuotaSettlementFunding{err: errors.New("wss settlement failed")},
		preConsumedQuota: 10,
	}
	usage := &dto.RealtimeUsage{
		InputTokens:  20,
		OutputTokens: 10,
		TotalTokens:  30,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 20,
		},
		OutputTokenDetails: dto.OutputTokenDetails{
			TextTokens: 10,
		},
	}

	err := PostWssConsumeQuota(ctx, relayInfo, "gpt-4o-realtime-preview", usage, "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "wss settlement failed")
}

func TestPostAudioConsumeQuotaKeepsCalculatedQuotaWhenTotalTokensMissing(t *testing.T) {
	truncate(t)
	const userID = 9570
	const tokenID = 9571
	const channelID = 9572
	const preConsumed = 50
	seedUser(t, userID, 10000-preConsumed)
	seedChannel(t, channelID)
	seedToken(t, tokenID, userID, "audio-zero-total", 100000)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "audio-zero-total-owner")
	ctx.Set("token_name", "audio-zero-total")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "audio-zero-total",
		OriginModelName: "gpt-4o-audio-preview",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}
	usage := &dto.Usage{
		TotalTokens: 0,
		PromptTokensDetails: dto.InputTokenDetails{
			AudioTokens: 80,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{
			AudioTokens: 40,
		},
	}

	err := PostAudioConsumeQuota(ctx, relayInfo, usage, "")
	require.NoError(t, err)
	require.EqualValues(t, 10000-1920, getUserQuota(t, userID))
}

func TestPostAudioConsumeQuotaKeepsFixedPriceWhenUsageMissing(t *testing.T) {
	truncate(t)
	const userID = 9573
	const tokenID = 9574
	const channelID = 9575
	const preConsumed = 40
	seedUser(t, userID, 10000-preConsumed)
	seedChannel(t, channelID)
	seedToken(t, tokenID, userID, "audio-fixed-zero", 1000-preConsumed)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "audio-fixed-zero-owner")
	ctx.Set("token_name", "audio-fixed-zero")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "audio-fixed-zero",
		OriginModelName: "gpt-4o-audio-preview",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			UsePrice:       true,
			ModelPrice:     0.00008,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}

	err := PostAudioConsumeQuota(ctx, relayInfo, &dto.Usage{}, "")
	require.NoError(t, err)
	require.EqualValues(t, 10000-preConsumed, getUserQuota(t, userID))
	require.EqualValues(t, 1000-preConsumed, getTokenRemainQuota(t, tokenID))
}

func TestPostWssConsumeQuotaKeepsSettlementWhenConsumeLogFails(t *testing.T) {
	truncate(t)
	useBrokenLogDB(t)

	const userID = 9580
	const tokenID = 9581
	const channelID = 9582
	const preConsumed = 50
	seedUser(t, userID, 10000-preConsumed)
	seedChannel(t, channelID)
	seedToken(t, tokenID, userID, "wss-log-fail", 1000-preConsumed)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "wss-log-fail-owner")
	ctx.Set("token_name", "wss-log-fail")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "wss-log-fail",
		OriginModelName: "gpt-4o-realtime-preview",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}

	err := PostWssConsumeQuota(ctx, relayInfo, "gpt-4o-realtime-preview", &dto.RealtimeUsage{
		TotalTokens: 50,
		InputTokens: 50,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 50,
		},
	}, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "record consume log failed")
	require.EqualValues(t, 10000-preConsumed, getUserQuota(t, userID))
	require.EqualValues(t, 1000-preConsumed, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	require.EqualValues(t, preConsumed, usedQuota)
	require.Equal(t, 1, requestCount)
	require.EqualValues(t, int64(preConsumed), getChannelUsedQuota(t, channelID))
}

func TestPostAudioConsumeQuotaKeepsSettlementWhenConsumeLogFails(t *testing.T) {
	truncate(t)
	useBrokenLogDB(t)

	const userID = 9590
	const tokenID = 9591
	const channelID = 9592
	const preConsumed = 50
	seedUser(t, userID, 10000-preConsumed)
	seedChannel(t, channelID)
	seedToken(t, tokenID, userID, "audio-log-fail", 1000-preConsumed)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "audio-log-fail-owner")
	ctx.Set("token_name", "audio-log-fail")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "audio-log-fail",
		OriginModelName: "gpt-4o-audio-preview",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}
	usage := &dto.Usage{
		PromptTokens:     50,
		CompletionTokens: 0,
		TotalTokens:      50,
		PromptTokensDetails: dto.InputTokenDetails{
			AudioTokens: 50,
		},
	}

	err := PostAudioConsumeQuota(ctx, relayInfo, usage, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "record consume log failed")
	expectedQuota, _ := calculateAudioQuota(QuotaInfo{
		InputDetails: TokenDetails{AudioTokens: usage.PromptTokensDetails.AudioTokens},
		ModelName:    relayInfo.OriginModelName,
		ModelRatio:   relayInfo.PriceData.ModelRatio,
		GroupRatio:   relayInfo.PriceData.GroupRatioInfo.GroupRatio,
	})
	require.Positive(t, expectedQuota)
	require.EqualValues(t, 10000-expectedQuota, getUserQuota(t, userID))
	require.EqualValues(t, 1000-expectedQuota, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	require.EqualValues(t, expectedQuota, usedQuota)
	require.Equal(t, 1, requestCount)
	require.EqualValues(t, int64(expectedQuota), getChannelUsedQuota(t, channelID))
}

func TestPostTextConsumeQuotaKeepsFixedPriceWhenUsageMissingAfterDelivery(t *testing.T) {
	truncate(t)
	const userID = 9610
	const tokenID = 9611
	const channelID = 9612
	const preConsumed = 40
	seedUser(t, userID, 10000-preConsumed)
	seedChannel(t, channelID)
	seedToken(t, tokenID, userID, "text-zero-usage", 1000-preConsumed)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "text-zero-usage-owner")
	ctx.Set("token_name", "text-zero-usage")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "text-zero-usage",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			UsePrice:       true,
			ModelPrice:     0.00008,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}

	err := PostTextConsumeQuotaChecked(ctx, relayInfo, &dto.Usage{}, nil)
	require.NoError(t, err)
	require.EqualValues(t, 10000-preConsumed, getUserQuota(t, userID))
	require.EqualValues(t, 1000-preConsumed, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	require.EqualValues(t, preConsumed, usedQuota)
	require.Equal(t, 1, requestCount)
}

func TestPostTextConsumeQuotaKeepsReservationWhenTokenUsageMissingAfterDelivery(t *testing.T) {
	truncate(t)
	const userID = 9613
	const tokenID = 9614
	const channelID = 9615
	const preConsumed = 40
	seedUser(t, userID, 10000-preConsumed)
	seedChannel(t, channelID)
	seedToken(t, tokenID, userID, "text-zero-ratio", 1000-preConsumed)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "text-zero-ratio-owner")
	ctx.Set("token_name", "text-zero-ratio")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "text-zero-ratio",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}

	err := PostTextConsumeQuotaChecked(ctx, relayInfo, &dto.Usage{}, nil)
	require.NoError(t, err)
	require.EqualValues(t, 10000-preConsumed, getUserQuota(t, userID))
	require.EqualValues(t, 1000-preConsumed, getTokenRemainQuota(t, tokenID))
}

func TestPostAudioConsumeQuotaKeepsReservationWhenUsageMissingAfterDelivery(t *testing.T) {
	truncate(t)
	const userID = 9616
	const tokenID = 9617
	const preConsumed = 40
	seedUser(t, userID, 10000-preConsumed)
	seedChannel(t, 9618)
	seedToken(t, tokenID, userID, "audio-zero-ratio", 1000-preConsumed)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "audio-zero-ratio-owner")
	ctx.Set("token_name", "audio-zero-ratio")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 9618},
		TokenId:         tokenID,
		TokenKey:        "audio-zero-ratio",
		OriginModelName: "gpt-4o-audio",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}

	err := PostAudioConsumeQuota(ctx, relayInfo, &dto.Usage{}, "")
	require.NoError(t, err)
	require.EqualValues(t, 10000-preConsumed, getUserQuota(t, userID))
	require.EqualValues(t, 1000-preConsumed, getTokenRemainQuota(t, tokenID))
}

func TestPostWssConsumeQuotaRefundsReservedQuotaWhenUsageMissing(t *testing.T) {
	truncate(t)
	seedUser(t, 9560, 10000-40)
	seedChannel(t, 9561)
	seedToken(t, 9562, 9560, "wss-zero-token", 1000)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "wss-zero-owner")
	ctx.Set("token_name", "wss-zero-token")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          9560,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 9561},
		TokenId:         9562,
		TokenKey:        "wss-zero-token",
		OriginModelName: "gpt-4o-realtime-preview",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: 9560, consumed: 40},
		preConsumedQuota: 40,
	}

	err := PostWssConsumeQuota(ctx, relayInfo, "gpt-4o-realtime-preview", &dto.RealtimeUsage{}, "")
	require.NoError(t, err)
	require.EqualValues(t, 10000, getUserQuota(t, 9560))
}

func TestPostWssConsumeQuotaKeepsFixedPriceWhenUsageMissing(t *testing.T) {
	truncate(t)
	const userID = 9563
	const preConsumed = 40
	seedUser(t, userID, 10000-preConsumed)
	seedChannel(t, 9564)
	seedToken(t, 9565, userID, "wss-fixed-zero", 1000-preConsumed)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "wss-fixed-zero-owner")
	ctx.Set("token_name", "wss-fixed-zero")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 9564},
		TokenId:         9565,
		TokenKey:        "wss-fixed-zero",
		OriginModelName: "gpt-4o-realtime-preview",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			UsePrice:       true,
			ModelPrice:     0.00008,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}

	err := PostWssConsumeQuota(ctx, relayInfo, "gpt-4o-realtime-preview", &dto.RealtimeUsage{}, "")
	require.NoError(t, err)
	require.EqualValues(t, 10000-preConsumed, getUserQuota(t, userID))
	require.EqualValues(t, 1000-preConsumed, getTokenRemainQuota(t, 9565))
}

func TestPostTextConsumeQuotaCheckedReturnsErrorAndSkipsLogWhenUsageCounterUpdateFails(t *testing.T) {
	truncate(t)
	oldDataExportEnabled := common.DataExportEnabled
	common.DataExportEnabled = true
	t.Cleanup(func() {
		common.DataExportEnabled = oldDataExportEnabled
		model.CacheQuotaDataLock.Lock()
		model.CacheQuotaData = make(map[string]*model.QuotaData)
		model.CacheQuotaDataLock.Unlock()
	})
	model.CacheQuotaDataLock.Lock()
	model.CacheQuotaData = make(map[string]*model.QuotaData)
	model.CacheQuotaDataLock.Unlock()
	seedUser(t, 9502, 10000)
	seedToken(t, 9503, 9502, "usage-counter-token", 1000)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "usage-counter-owner")
	ctx.Set("token_name", "usage-counter-token")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          9502,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 9504},
		TokenId:         9503,
		TokenKey:        "usage-counter-token",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &failingTextQuotaSettlementFunding{},
		preConsumedQuota: 10,
	}
	usage := &dto.Usage{
		PromptTokens:     20,
		CompletionTokens: 10,
		TotalTokens:      30,
	}

	err := PostTextConsumeQuotaChecked(ctx, relayInfo, usage, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "usage counter update failed")
	assert.Equal(t, int64(0), countLogs(t))
}

func TestPostTextConsumeQuotaRecordsAccountingErrorWhenCallerIgnoresFailure(t *testing.T) {
	truncate(t)
	const userID = 9512
	const tokenID = 9513
	const missingChannelID = 9514
	const initialUserQuota = 10000
	const initialTokenRemain = 1000
	const preConsumed = 10
	seedUser(t, userID, initialUserQuota-preConsumed)
	seedToken(t, tokenID, userID, "unchecked-accounting-token", initialTokenRemain-preConsumed)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", preConsumed).Error)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "unchecked-accounting-owner")
	ctx.Set("token_name", "unchecked-accounting-token")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: missingChannelID},
		TokenId:         tokenID,
		TokenKey:        "unchecked-accounting-token",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}
	usage := &dto.Usage{
		PromptTokens:     20,
		CompletionTokens: 10,
		TotalTokens:      30,
	}

	PostTextConsumeQuota(ctx, relayInfo, usage, nil)

	const charged = 30
	assert.EqualValues(t, initialUserQuota-charged, getUserQuota(t, userID))
	assert.EqualValues(t, initialTokenRemain-charged, getTokenRemainQuota(t, tokenID))
	assert.EqualValues(t, charged, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.EqualValues(t, 0, usedQuota)
	assert.Equal(t, 0, requestCount)
	var consumeCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("user_id = ? AND type = ?", userID, model.LogTypeConsume).Count(&consumeCount).Error)
	require.Zero(t, consumeCount)
	var errorLog model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", userID, model.LogTypeError).First(&errorLog).Error)
	require.Equal(t, "unchecked-accounting-owner", errorLog.Username)
	require.Equal(t, "gpt-4o", errorLog.ModelName)
	require.Equal(t, tokenID, errorLog.TokenId)
	require.Contains(t, errorLog.Content, "post text consume quota failed")
	require.Contains(t, errorLog.Other, "accounting_error")
}

func TestPostWssConsumeQuotaKeepsSettlementWhenUsageCounterUpdateFails(t *testing.T) {
	truncate(t)
	const userID = 9701
	const tokenID = 9702
	const missingChannelID = 9703
	const initialUserQuota = 10000
	const initialTokenRemain = 1000
	const preConsumed = 10
	seedUser(t, userID, initialUserQuota-preConsumed)
	seedToken(t, tokenID, userID, "wss-counter-fail", initialTokenRemain-preConsumed)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", preConsumed).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "wss-counter-fail-owner")
	ctx.Set("token_name", "wss-counter-fail")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: missingChannelID},
		TokenId:         tokenID,
		TokenKey:        "wss-counter-fail",
		OriginModelName: "gpt-4o-realtime-preview",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}

	err := PostWssConsumeQuota(ctx, relayInfo, relayInfo.OriginModelName, &dto.RealtimeUsage{
		TotalTokens:  30,
		InputTokens:  20,
		OutputTokens: 10,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 20,
		},
		OutputTokenDetails: dto.OutputTokenDetails{
			TextTokens: 10,
		},
	}, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage counter update failed")
	expectedQuota, _ := calculateAudioQuota(QuotaInfo{
		InputDetails:  TokenDetails{TextTokens: 20},
		OutputDetails: TokenDetails{TextTokens: 10},
		ModelName:     relayInfo.OriginModelName,
		ModelRatio:    1,
		GroupRatio:    1,
	})
	require.Positive(t, expectedQuota)
	require.EqualValues(t, initialUserQuota-expectedQuota, getUserQuota(t, userID))
	require.EqualValues(t, initialTokenRemain-expectedQuota, getTokenRemainQuota(t, tokenID))
	require.EqualValues(t, expectedQuota, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	require.EqualValues(t, 0, usedQuota)
	require.Equal(t, 0, requestCount)
}

func TestPostAudioConsumeQuotaKeepsSettlementWhenUsageCounterUpdateFails(t *testing.T) {
	truncate(t)
	const userID = 9704
	const tokenID = 9705
	const missingChannelID = 9706
	const initialUserQuota = 10000
	const initialTokenRemain = 1000
	const preConsumed = 50
	seedUser(t, userID, initialUserQuota-preConsumed)
	seedToken(t, tokenID, userID, "audio-counter-fail", initialTokenRemain-preConsumed)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", preConsumed).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "audio-counter-fail-owner")
	ctx.Set("token_name", "audio-counter-fail")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: missingChannelID},
		TokenId:         tokenID,
		TokenKey:        "audio-counter-fail",
		OriginModelName: "gpt-4o-audio-preview",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}
	usage := &dto.Usage{
		PromptTokens: 50,
		TotalTokens:  50,
		PromptTokensDetails: dto.InputTokenDetails{
			AudioTokens: 50,
		},
	}

	err := PostAudioConsumeQuota(ctx, relayInfo, usage, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage counter update failed")
	expectedQuota, _ := calculateAudioQuota(QuotaInfo{
		InputDetails: TokenDetails{AudioTokens: 50},
		ModelName:    relayInfo.OriginModelName,
		ModelRatio:   1,
		GroupRatio:   1,
	})
	require.Positive(t, expectedQuota)
	require.EqualValues(t, initialUserQuota-expectedQuota, getUserQuota(t, userID))
	require.EqualValues(t, initialTokenRemain-expectedQuota, getTokenRemainQuota(t, tokenID))
	require.EqualValues(t, expectedQuota, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	require.EqualValues(t, 0, usedQuota)
	require.Equal(t, 0, requestCount)
}

func TestPostTextConsumeQuotaCheckedKeepsSettlementWhenConsumeLogFails(t *testing.T) {
	truncate(t)
	useBrokenLogDB(t)

	const userID = 9520
	const tokenID = 9521
	const channelID = 9522
	const initialUserQuota = 10000
	const initialTokenRemain = 1000
	const preConsumed = 10

	seedUser(t, userID, initialUserQuota-preConsumed)
	seedToken(t, tokenID, userID, "text-log-fail-token", initialTokenRemain-preConsumed)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", preConsumed).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "text-log-fail-owner")
	ctx.Set("token_name", "text-log-fail-token")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "text-log-fail-token",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}
	usage := &dto.Usage{
		PromptTokens:     20,
		CompletionTokens: 10,
		TotalTokens:      30,
	}

	err := PostTextConsumeQuotaChecked(ctx, relayInfo, usage, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "record consume log failed")
	assert.EqualValues(t, initialUserQuota-30, getUserQuota(t, userID))
	assert.EqualValues(t, initialTokenRemain-30, getTokenRemainQuota(t, tokenID))
	assert.EqualValues(t, 30, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.EqualValues(t, 30, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.EqualValues(t, int64(30), getChannelUsedQuota(t, channelID))
}

func TestPostTextConsumeQuotaRecoversUsageAndLogAfterCounterFailure(t *testing.T) {
	truncate(t)
	const userID = 9711
	const tokenID = 9712
	const channelID = 9713
	const initialUserQuota = 10000
	const initialTokenRemain = 1000
	const preConsumed = 10
	seedUser(t, userID, initialUserQuota-preConsumed)
	seedToken(t, tokenID, userID, "text-audit-usage", initialTokenRemain-preConsumed)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", preConsumed).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "text-audit-usage-owner")
	ctx.Set("token_name", "text-audit-usage")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "text-audit-usage",
		RequestId:       "req-text-audit-usage",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}

	err := PostTextConsumeQuotaChecked(ctx, relayInfo, &dto.Usage{
		PromptTokens:     20,
		CompletionTokens: 10,
		TotalTokens:      30,
	}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage counter update failed")
	require.EqualValues(t, initialUserQuota-30, getUserQuota(t, userID))
	require.EqualValues(t, initialTokenRemain-30, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	require.EqualValues(t, 0, usedQuota)
	require.Equal(t, 0, requestCount)
	require.Equal(t, int64(0), countConsumeLogs(t, userID))

	seedChannel(t, channelID)
	require.NoError(t, model.RecoverPendingConsumptionAudits(10))
	require.NoError(t, model.RecoverPendingConsumptionAudits(10))
	usedQuota, requestCount = getUserUsageCounters(t, userID)
	require.EqualValues(t, 30, usedQuota)
	require.Equal(t, 1, requestCount)
	require.EqualValues(t, int64(30), getChannelUsedQuota(t, channelID))
	require.Equal(t, int64(1), countConsumeLogs(t, userID))
	require.EqualValues(t, initialUserQuota-30, getUserQuota(t, userID))
	require.EqualValues(t, initialTokenRemain-30, getTokenRemainQuota(t, tokenID))
}

func TestPostTextConsumeQuotaRecoversLogAfterConsumeLogFailure(t *testing.T) {
	truncate(t)
	useBrokenLogDB(t)
	const userID = 9721
	const tokenID = 9722
	const channelID = 9723
	const initialUserQuota = 10000
	const initialTokenRemain = 1000
	const preConsumed = 10
	seedUser(t, userID, initialUserQuota-preConsumed)
	seedToken(t, tokenID, userID, "text-audit-log", initialTokenRemain-preConsumed)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", preConsumed).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "text-audit-log-owner")
	ctx.Set("token_name", "text-audit-log")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "text-audit-log",
		RequestId:       "req-text-audit-log",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}

	err := PostTextConsumeQuotaChecked(ctx, relayInfo, &dto.Usage{
		PromptTokens:     20,
		CompletionTokens: 10,
		TotalTokens:      30,
	}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "record consume log failed")
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	require.EqualValues(t, 30, usedQuota)
	require.Equal(t, 1, requestCount)
	require.Equal(t, int64(0), countConsumeLogs(t, userID))

	model.LOG_DB = model.DB
	require.NoError(t, model.RecoverPendingConsumptionAudits(10))
	require.NoError(t, model.RecoverPendingConsumptionAudits(10))
	require.Equal(t, int64(1), countConsumeLogs(t, userID))
	logItem := getLastLog(t)
	require.NotNil(t, logItem)
	require.Equal(t, 30, logItem.Quota)
	require.Equal(t, "audit:req-text-audit-log", logItem.SettlementKey)
	usedQuota, requestCount = getUserUsageCounters(t, userID)
	require.EqualValues(t, 30, usedQuota)
	require.Equal(t, 1, requestCount)
	require.EqualValues(t, initialUserQuota-30, getUserQuota(t, userID))
}

func countConsumeLogs(t *testing.T, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.Log{}).Where("user_id = ? AND type = ?", userID, model.LogTypeConsume).Count(&count).Error)
	return count
}

func TestPostTextConsumeQuotaCheckedDefersMissingSubscriptionAndRecoversOnce(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID, subscriptionID = 9630, 9631, 9632, 9633
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "text-deferred-token", 990)
	seedChannel(t, channelID)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "text-deferred-owner")
	ctx.Set("token_name", "text-deferred-token")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "text-deferred-token",
		RequestId:       "req-text-deferred",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	session := &BillingSession{
		relayInfo: relayInfo,
		funding: &SubscriptionFunding{
			requestId:      relayInfo.RequestId,
			subscriptionId: subscriptionID,
			preConsumed:    10,
		},
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}
	relayInfo.Billing = session

	err := PostTextConsumeQuotaChecked(ctx, relayInfo, &dto.Usage{
		PromptTokens:     20,
		CompletionTokens: 10,
		TotalTokens:      30,
	}, nil)
	require.NoError(t, err)
	require.EqualValues(t, 10000, getUserQuota(t, userID))
	require.EqualValues(t, 990, getTokenRemainQuota(t, tokenID))
	logItem := getLastLog(t)
	require.NotNil(t, logItem)
	require.Equal(t, 30, logItem.Quota)

	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          9634,
		Title:       "text-deferred",
		PriceAmount: 1,
	}).Error)
	seedSubscription(t, subscriptionID, userID, 1000, 10)
	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.EqualValues(t, 30, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 970, getTokenRemainQuota(t, tokenID))
}

// TestTryTieredSettleRecordsClampOnOverflow guards that an oversized tiered
// settlement both saturates the quota and records the clamp on RelayInfo, so
// every consume path (text, audio, WSS) can surface it under admin_info.
func TestTryTieredSettleRecordsClampOnOverflow(t *testing.T) {
	// exprOutput = p * 1e9; quotaBeforeGroup = p*1e9 / 1e6 * 5e5 far exceeds
	// MaxInt32 and must saturate.
	exprStr := `tier("base", p * 1000000000)`
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "overflow-model",
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:  "tiered_expr",
			ExprString:   exprStr,
			ExprHash:     billingexpr.ExprHashString(exprStr),
			GroupRatio:   1,
			QuotaPerUnit: 500_000,
		},
	}

	ok, quota, result := TryTieredSettle(relayInfo, billingexpr.TokenParams{P: 1_000_000_000})

	require.True(t, ok)
	require.NotNil(t, result)
	require.EqualValues(t, math.MaxInt32, quota, "oversized settlement must clamp, never wrap negative")
	require.NotNil(t, relayInfo.QuotaClamp, "clamp must be recorded on RelayInfo for admin auditing")
	require.EqualValues(t, common.QuotaClampOverflow, relayInfo.QuotaClamp.Kind)
}

// TestTryTieredSettleNoClampInRange confirms an in-range settlement leaves
// RelayInfo.QuotaClamp nil.
func TestTryTieredSettleNoClampInRange(t *testing.T) {
	exprStr := `tier("base", p * 2 + c * 10)`
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "in-range-model",
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:  "tiered_expr",
			ExprString:   exprStr,
			ExprHash:     billingexpr.ExprHashString(exprStr),
			GroupRatio:   1,
			QuotaPerUnit: 500_000,
		},
	}

	ok, _, result := TryTieredSettle(relayInfo, billingexpr.TokenParams{P: 1000, C: 500})

	require.True(t, ok)
	require.NotNil(t, result)
	require.Nil(t, relayInfo.QuotaClamp, "in-range settlement must not record a clamp")
}

func TestCalculateTextQuotaSummaryKeepsToolSurchargeWhenUsageTokensAreZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4o",
		PriceData: types.PriceData{
			ModelRatio: 1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {ToolName: dto.BuildInToolWebSearchPreview, CallCount: 1},
			},
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, &dto.Usage{})
	require.Equal(t, 1, summary.WebSearchCallCount)
	require.Greater(t, summary.Quota, 0)
}

func TestCalculateTextQuotaSummaryFixedPriceAppliesImageCountOnceAndAllowsOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	priceData := types.PriceData{
		ModelPrice: 0.12,
		UsePrice:   true,
		GroupRatioInfo: types.GroupRatioInfo{
			GroupRatio: 1,
		},
	}
	priceData.AddOtherRatio("n", 3)
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "dall-e-3",
		PriceData:       priceData,
		StartTime:       time.Now(),
	}
	usage := &dto.Usage{PromptTokens: 1, TotalTokens: 1}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.EqualValues(t, 180000, summary.Quota)

	// An adaptor-reported actual count replaces the requested count rather
	// than multiplying it a second time.
	relayInfo.PriceData.AddOtherRatio("n", 2)
	summary = calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.EqualValues(t, 120000, summary.Quota)
}
