package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	// Import oauth package to register providers via init()
	_ "github.com/QuantumNous/new-api/oauth"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func SetApiRouter(router *gin.Engine) {
	apiRouter := router.Group("/api")
	apiRouter.Use(middleware.RouteTag("api"))
	apiRouter.Use(gzip.Gzip(gzip.DefaultCompression))
	apiRouter.Use(middleware.BodyStorageCleanup()) // 清理请求体存储
	apiRouter.Use(middleware.GlobalAPIRateLimit())
	anonymousRequestBodyLimit := middleware.AnonymousRequestBodyLimit()
	{
		apiRouter.GET("/setup", controller.GetSetup)
		apiRouter.POST("/setup", anonymousRequestBodyLimit, controller.PostSetup)
		apiRouter.GET("/status", controller.GetStatus)
		apiRouter.GET("/uptime/status", controller.GetUptimeKumaStatus)
		apiRouter.GET("/models", middleware.UserAuth(), controller.DashboardListModels)
		apiRouter.GET("/status/test", middleware.AdminAuth(), controller.TestStatus)
		apiRouter.GET("/notice", controller.GetNotice)
		apiRouter.GET("/user-agreement", controller.GetUserAgreement)
		apiRouter.GET("/privacy-policy", controller.GetPrivacyPolicy)
		apiRouter.GET("/about", controller.GetAbout)
		apiRouter.GET("/r/:code", controller.ReferralLanding)
		apiRouter.GET("/referral-assets/*path", controller.GetReferralAsset)
		//apiRouter.GET("/midjourney", controller.GetMidjourney)
		apiRouter.GET("/home_page_content", controller.GetHomePageContent)
		apiRouter.GET("/pricing", middleware.HeaderNavModuleAuth("pricing"), controller.GetPricing)
		apiRouter.GET("/provider/pricing", controller.GetPublicProviderPricing)
		apiRouter.OPTIONS("/provider/pricing", controller.GetPublicProviderPricing)
		perfMetricsRoute := apiRouter.Group("/perf-metrics")
		perfMetricsRoute.Use(middleware.HeaderNavModulePublicOrUserAuth("pricing"))
		{
			perfMetricsRoute.GET("/summary", controller.GetPerfMetricsSummary)
			perfMetricsRoute.GET("", controller.GetPerfMetrics)
		}
		apiRouter.GET("/rankings", middleware.HeaderNavModuleAuth("rankings"), controller.GetRankings)
		apiRouter.GET("/verification", middleware.EmailVerificationRateLimit(), middleware.TurnstileCheck(), controller.SendEmailVerification)
		apiRouter.GET("/reset_password", middleware.SetupRequired(), middleware.EmailQueryRateLimit("CT:password-reset-email"), middleware.TurnstileCheck(), controller.SendPasswordResetEmail)
		apiRouter.POST("/user/reset", middleware.SetupRequired(), anonymousRequestBodyLimit, middleware.EmailBodyCriticalRateLimit("CT:password-reset-submit"), controller.ResetPassword)
		// OAuth routes - specific routes must come before :provider wildcard
		apiRouter.GET("/oauth/state", middleware.SetupRequired(), middleware.SessionNonceCriticalRateLimit("CT:oauth-state", "rate_limit_oauth_state"), controller.GenerateOAuthCode)
		apiRouter.POST("/oauth/email/bind", middleware.SetupRequired(), middleware.UserAuth(), anonymousRequestBodyLimit, middleware.UserCriticalRateLimit("CT:oauth-email-bind"), controller.EmailBind)
		// Non-standard OAuth (WeChat, Telegram) - keep original routes
		apiRouter.GET("/oauth/wechat", middleware.SetupRequired(), middleware.SessionNonceCriticalRateLimit("CT:oauth-wechat-login", "rate_limit_oauth_wechat"), controller.WeChatAuth)
		apiRouter.POST("/oauth/wechat/bind", middleware.SetupRequired(), middleware.UserAuth(), anonymousRequestBodyLimit, middleware.UserCriticalRateLimit("CT:oauth-wechat-bind"), controller.WeChatBind)
		apiRouter.GET("/oauth/telegram/login", middleware.SetupRequired(), middleware.TelegramCriticalRateLimit("CT:oauth-telegram-login"), controller.TelegramLogin)
		apiRouter.GET("/oauth/telegram/bind", middleware.SetupRequired(), middleware.SessionUserCriticalRateLimit("CT:oauth-telegram-bind"), controller.TelegramBind)
		// Standard OAuth providers (GitHub, Discord, OIDC, LinuxDO) - unified route
		apiRouter.GET("/oauth/:provider", middleware.SetupRequired(), middleware.SessionFieldCriticalRateLimit("CT:oauth-callback", "oauth_state"), controller.HandleOAuth)
		apiRouter.GET("/ratio_config", middleware.SessionNonceCriticalRateLimit("CT:ratio-config", "rate_limit_ratio_config"), controller.GetRatioConfig)

		apiRouter.POST("/stripe/webhook", anonymousRequestBodyLimit, controller.StripeWebhook)
		apiRouter.POST("/creem/webhook", anonymousRequestBodyLimit, controller.CreemWebhook)
		apiRouter.POST("/waffo/webhook", anonymousRequestBodyLimit, controller.WaffoWebhook)
		// :env separates test vs prod URLs so the operator can register each
		// in Pancake's matching webhook slot; handler enforces env match.
		apiRouter.POST("/waffo-pancake/webhook/:env", anonymousRequestBodyLimit, controller.WaffoPancakeWebhook)

		// Universal secure verification routes
		apiRouter.POST("/verify", middleware.UserAuth(), middleware.UserCriticalRateLimit("CT:secure-verify"), controller.UniversalVerify)

		userRoute := apiRouter.Group("/user")
		{
			userRoute.POST("/register", middleware.SetupRequired(), anonymousRequestBodyLimit, middleware.RegisterCriticalRateLimit("CT:user-register"), middleware.TurnstileCheck(), controller.Register)
			userRoute.POST("/login", middleware.SetupRequired(), anonymousRequestBodyLimit, middleware.UsernameCriticalRateLimit("CT:user-login"), middleware.TurnstileCheck(), controller.Login)
			userRoute.POST("/login/2fa", middleware.SetupRequired(), anonymousRequestBodyLimit, middleware.SessionFieldCriticalRateLimit("CT:user-login-2fa", "pending_user_id"), controller.Verify2FALogin)
			userRoute.POST("/passkey/login/begin", middleware.SetupRequired(), anonymousRequestBodyLimit, middleware.SessionNonceCriticalRateLimit("CT:passkey-login-begin", "rate_limit_passkey_login_begin"), controller.PasskeyLoginBegin)
			userRoute.POST("/passkey/login/finish", middleware.SetupRequired(), anonymousRequestBodyLimit, middleware.SessionFieldCriticalRateLimit("CT:passkey-login-finish", "passkey_login_session"), controller.PasskeyLoginFinish)
			//userRoute.POST("/tokenlog", middleware.UserCriticalRateLimit("CT:token-log"), controller.TokenLog)
			userRoute.GET("/logout", controller.Logout)
			userRoute.POST("/epay/notify", anonymousRequestBodyLimit, controller.EpayNotify)
			userRoute.GET("/epay/notify", controller.EpayNotify)
			userRoute.POST("/epay/return", anonymousRequestBodyLimit, controller.EpayReturn)
			userRoute.GET("/epay/return", controller.EpayReturn)
			userRoute.POST("/bepusdt/notify", anonymousRequestBodyLimit, controller.BEpusdtTopUpNotify)
			userRoute.GET("/bepusdt/notify", controller.BEpusdtTopUpNotify)
			userRoute.GET("/groups", controller.GetUserGroups)

			selfRoute := userRoute.Group("/")
			selfRoute.Use(middleware.UserAuth())
			{
				selfRoute.GET("/self/groups", controller.GetUserGroups)
				selfRoute.GET("/self", controller.GetSelf)
				selfRoute.GET("/models", controller.GetUserModels)
				selfRoute.PUT("/self", controller.UpdateSelf)
				selfRoute.DELETE("/self", controller.DeleteSelf)
				selfRoute.GET("/token", controller.GenerateAccessToken)
				selfRoute.GET("/passkey", controller.PasskeyStatus)
				selfRoute.POST("/passkey/register/begin", controller.PasskeyRegisterBegin)
				selfRoute.POST("/passkey/register/finish", controller.PasskeyRegisterFinish)
				selfRoute.POST("/passkey/verify/begin", controller.PasskeyVerifyBegin)
				selfRoute.POST("/passkey/verify/finish", controller.PasskeyVerifyFinish)
				selfRoute.DELETE("/passkey", controller.PasskeyDelete)
				selfRoute.GET("/referral/profile", controller.GetReferralProfile)
				selfRoute.GET("/referral/summary", controller.GetReferralSummary)
				selfRoute.POST("/referral/apply", controller.ApplyReferralAffiliate)
				selfRoute.GET("/referral/commissions", controller.GetReferralCommissions)
				selfRoute.GET("/referral/withdrawals", controller.GetReferralWithdrawals)
				selfRoute.POST("/referral/withdrawals", controller.CreateReferralWithdrawal)
				selfRoute.POST("/referral/withdrawals/:id/cancel", controller.CancelReferralWithdrawal)
				selfRoute.POST("/referral/upload", controller.UploadReferralAsset)
				selfRoute.GET("/topup/info", controller.GetTopUpInfo)
				selfRoute.GET("/topup/self", controller.GetUserTopUps)
				selfRoute.POST("/topup", middleware.UserCriticalRateLimit("CT:topup-code"), controller.TopUp)
				selfRoute.POST("/pay", middleware.UserCriticalRateLimit("CT:pay-epay"), controller.RequestEpay)
				selfRoute.POST("/amount", controller.RequestAmount)
				selfRoute.POST("/stripe/pay", middleware.UserCriticalRateLimit("CT:pay-stripe"), controller.RequestStripePay)
				selfRoute.POST("/stripe/amount", controller.RequestStripeAmount)
				selfRoute.POST("/creem/pay", middleware.UserCriticalRateLimit("CT:pay-creem"), controller.RequestCreemPay)
				selfRoute.GET("/bepusdt/assets", controller.GetBEpusdtAssets)
				selfRoute.POST("/bepusdt/pay", middleware.UserCriticalRateLimit("CT:pay-bepusdt"), controller.RequestBEpusdtPay)
				selfRoute.POST("/waffo/amount", controller.RequestWaffoAmount)
				selfRoute.POST("/waffo/pay", middleware.UserCriticalRateLimit("CT:pay-waffo"), controller.RequestWaffoPay)
				selfRoute.POST("/waffo-pancake/amount", controller.RequestWaffoPancakeAmount)
				selfRoute.POST("/waffo-pancake/pay", middleware.UserCriticalRateLimit("CT:pay-waffo-pancake"), controller.RequestWaffoPancakePay)
				selfRoute.PUT("/setting", controller.UpdateUserSetting)
				selfRoute.GET("/tickets", controller.ListTickets)
				selfRoute.POST("/tickets", middleware.UploadRateLimit(), controller.CreateTicket)
				selfRoute.GET("/tickets/badge", controller.GetTicketBadge)
				selfRoute.GET("/tickets/:id/attachments/:attachment_id", controller.GetTicketAttachment)
				selfRoute.GET("/tickets/:id", controller.GetTicket)
				selfRoute.POST("/tickets/:id/reply", middleware.UploadRateLimit(), controller.ReplyTicket)
				selfRoute.POST("/tickets/:id/close", controller.CloseTicket)
				selfRoute.POST("/tickets/:id/reopen", controller.ReopenTicket)

				// 2FA routes
				selfRoute.GET("/2fa/status", controller.Get2FAStatus)
				selfRoute.POST("/2fa/setup", controller.Setup2FA)
				selfRoute.POST("/2fa/enable", controller.Enable2FA)
				selfRoute.POST("/2fa/disable", controller.Disable2FA)
				selfRoute.POST("/2fa/backup_codes", controller.RegenerateBackupCodes)

				// Check-in routes
				selfRoute.GET("/checkin", controller.GetCheckinStatus)
				selfRoute.POST("/checkin", middleware.TurnstileCheck(), controller.DoCheckin)

				// Custom OAuth bindings
				selfRoute.GET("/oauth/bindings", controller.GetUserOAuthBindings)
				selfRoute.DELETE("/oauth/bindings/:provider_id", controller.UnbindCustomOAuth)
			}

			adminRoute := userRoute.Group("/")
			adminRoute.Use(middleware.AdminAuth())
			{
				adminRoute.GET("/", controller.GetAllUsers)
				adminRoute.GET("/topup", controller.GetAllTopUps)
				adminRoute.POST("/topup/complete", controller.AdminCompleteTopUp)
				adminRoute.GET("/admin/users/summary", controller.GetAdminUsersSummary)
				adminRoute.GET("/admin/finance/recharge-audit", controller.GetRechargeAudit)
				adminRoute.GET("/admin/finance/recharge-audit/summary", controller.GetRechargeAuditSummary)
				adminRoute.GET("/admin/referral/overview", controller.GetReferralOverview)
				adminRoute.GET("/admin/tickets", controller.ListTickets)
				adminRoute.GET("/admin/tickets/badge", controller.GetTicketBadge)
				adminRoute.GET("/admin/tickets/:id/attachments/:attachment_id", controller.GetTicketAttachment)
				adminRoute.GET("/admin/tickets/:id", controller.GetTicket)
				adminRoute.POST("/admin/tickets/:id/reply", middleware.UploadRateLimit(), controller.ReplyTicket)
				adminRoute.PUT("/admin/tickets/:id", controller.UpdateTicket)
				adminRoute.POST("/admin/tickets/:id/close", controller.CloseTicket)
				adminRoute.POST("/admin/tickets/:id/reopen", controller.ReopenTicket)
				adminRoute.GET("/admin/referral/badges", controller.GetReferralAdminBadges)
				adminRoute.GET("/admin/provider-pricing", controller.GetProviderPriceOverrides)
				adminRoute.PUT("/admin/provider-pricing", controller.UpdateProviderPriceOverrides)
				adminRoute.GET("/admin/referral/settings", controller.GetReferralSettings)
				adminRoute.PUT("/admin/referral/settings", controller.UpdateReferralSettings)
				adminRoute.GET("/admin/referral/pending", controller.GetPendingReferralAffiliates)
				adminRoute.GET("/admin/referral/affiliates", controller.GetReferralAffiliates)
				adminRoute.GET("/admin/referral/affiliates/:user_id/bindings", controller.GetReferralBindings)
				adminRoute.POST("/admin/referral/affiliates/:user_id/approve", controller.ApproveReferralAffiliate)
				adminRoute.POST("/admin/referral/affiliates/:user_id/rate", controller.SetReferralAffiliateRate)
				adminRoute.POST("/admin/referral/affiliates/:user_id/reject", controller.RejectReferralAffiliate)
				adminRoute.POST("/admin/referral/affiliates/:user_id/disable", controller.DisableReferralAffiliate)
				adminRoute.POST("/admin/referral/affiliates/:user_id/restore", controller.RestoreReferralAffiliate)
				adminRoute.POST("/admin/referral/affiliates/:user_id/adjust", controller.AdjustReferralAffiliate)
				adminRoute.POST("/admin/referral/affiliates/:user_id/settlement/freeze", controller.FreezeReferralSettlement)
				adminRoute.POST("/admin/referral/affiliates/:user_id/settlement/restore", controller.RestoreReferralSettlement)
				adminRoute.POST("/admin/referral/affiliates/:user_id/withdrawal/freeze", controller.FreezeReferralWithdrawal)
				adminRoute.POST("/admin/referral/affiliates/:user_id/withdrawal/restore", controller.RestoreReferralWithdrawal)
				adminRoute.GET("/admin/referral/commissions", controller.GetAdminReferralCommissions)
				adminRoute.GET("/admin/referral/commission-jobs", controller.GetReferralCommissionJobs)
				adminRoute.POST("/admin/referral/commission-jobs/retry", controller.RetryReferralCommissionJob)
				adminRoute.POST("/admin/referral/commission-jobs/backfill-redemptions", controller.BackfillRedemptionCommissionJobs)
				adminRoute.GET("/admin/referral/ledgers", controller.GetReferralLedgers)
				adminRoute.GET("/admin/referral/audit-logs", controller.GetReferralAdminAuditLogs)
				adminRoute.POST("/admin/referral/settlements/run", controller.RunReferralSettlementBatch)
				adminRoute.GET("/admin/referral/withdrawals", controller.GetAdminReferralWithdrawals)
				adminRoute.POST("/admin/referral/withdrawals/:id/approve", controller.ApproveReferralWithdrawal)
				adminRoute.POST("/admin/referral/withdrawals/:id/reject", controller.RejectReferralWithdrawal)
				adminRoute.POST("/admin/referral/withdrawals/:id/pay", controller.MarkReferralWithdrawalPaid)
				adminRoute.POST("/admin/referral/upload", controller.UploadReferralAsset)
				adminRoute.GET("/search", controller.SearchUsers)
				adminRoute.GET("/:id/oauth/bindings", controller.GetUserOAuthBindingsByAdmin)
				adminRoute.DELETE("/:id/oauth/bindings/:provider_id", controller.UnbindCustomOAuthByAdmin)
				adminRoute.DELETE("/:id/bindings/:binding_type", controller.AdminClearUserBinding)
				adminRoute.GET("/:id", controller.GetUser)
				adminRoute.POST("/", controller.CreateUser)
				adminRoute.POST("/manage", controller.ManageUser)
				adminRoute.PUT("/", controller.UpdateUser)
				adminRoute.DELETE("/:id", controller.DeleteUser)
				adminRoute.DELETE("/:id/reset_passkey", controller.AdminResetPasskey)

				// Admin 2FA routes
				adminRoute.GET("/2fa/stats", controller.Admin2FAStats)
				adminRoute.DELETE("/:id/2fa", controller.AdminDisable2FA)
			}
		}

		// Subscription billing (plans, purchase, admin management)
		subscriptionRoute := apiRouter.Group("/subscription")
		subscriptionRoute.Use(middleware.UserAuth())
		{
			subscriptionRoute.GET("/plans", controller.GetSubscriptionPlans)
			subscriptionRoute.GET("/self", controller.GetSubscriptionSelf)
			subscriptionRoute.PUT("/self/preference", controller.UpdateSubscriptionPreference)
			subscriptionRoute.POST("/balance/pay", middleware.UserCriticalRateLimit("CT:subscription-balance"), controller.SubscriptionRequestBalancePay)
			subscriptionRoute.POST("/epay/pay", middleware.UserCriticalRateLimit("CT:subscription-epay"), controller.SubscriptionRequestEpay)
			subscriptionRoute.POST("/stripe/pay", middleware.UserCriticalRateLimit("CT:subscription-stripe"), controller.SubscriptionRequestStripePay)
			subscriptionRoute.POST("/creem/pay", middleware.UserCriticalRateLimit("CT:subscription-creem"), controller.SubscriptionRequestCreemPay)
			subscriptionRoute.POST("/bepusdt/pay", middleware.UserCriticalRateLimit("CT:subscription-bepusdt"), controller.SubscriptionRequestBEpusdt)
			subscriptionRoute.POST("/waffo-pancake/pay", middleware.UserCriticalRateLimit("CT:subscription-waffo-pancake"), controller.SubscriptionRequestWaffoPancakePay)
		}
		subscriptionAdminRoute := apiRouter.Group("/subscription/admin")
		subscriptionAdminRoute.Use(middleware.AdminAuth())
		{
			subscriptionAdminRoute.GET("/plans", controller.AdminListSubscriptionPlans)
			subscriptionAdminRoute.POST("/plans", controller.AdminCreateSubscriptionPlan)
			subscriptionAdminRoute.PUT("/plans/:id", controller.AdminUpdateSubscriptionPlan)
			subscriptionAdminRoute.PATCH("/plans/:id", controller.AdminUpdateSubscriptionPlanStatus)
			subscriptionAdminRoute.POST("/bind", controller.AdminBindSubscription)

			// User subscription management (admin)
			subscriptionAdminRoute.GET("/users/:id/subscriptions", controller.AdminListUserSubscriptions)
			subscriptionAdminRoute.POST("/users/:id/subscriptions", controller.AdminCreateUserSubscription)
			subscriptionAdminRoute.POST("/user_subscriptions/:id/invalidate", controller.AdminInvalidateUserSubscription)
			subscriptionAdminRoute.DELETE("/user_subscriptions/:id", controller.AdminDeleteUserSubscription)
		}

		// Subscription payment callbacks (no auth)
		apiRouter.POST("/subscription/epay/notify", anonymousRequestBodyLimit, controller.SubscriptionEpayNotify)
		apiRouter.GET("/subscription/epay/notify", controller.SubscriptionEpayNotify)
		apiRouter.GET("/subscription/epay/return", controller.SubscriptionEpayReturn)
		apiRouter.POST("/subscription/epay/return", anonymousRequestBodyLimit, controller.SubscriptionEpayReturn)
		apiRouter.POST("/subscription/bepusdt/notify", anonymousRequestBodyLimit, controller.SubscriptionBEpusdtNotify)
		apiRouter.GET("/subscription/bepusdt/notify", controller.SubscriptionBEpusdtNotify)
		optionRoute := apiRouter.Group("/option")
		optionRoute.Use(middleware.RootAuth())
		{
			optionRoute.GET("/", controller.GetOptions)
			optionRoute.PUT("/", controller.UpdateOption)
			optionRoute.POST("/logo/upload", controller.UploadSystemLogo)
			optionRoute.POST("/payment_compliance", controller.ConfirmPaymentCompliance)
			optionRoute.GET("/channel_affinity_cache", controller.GetChannelAffinityCacheStats)
			optionRoute.DELETE("/channel_affinity_cache", controller.ClearChannelAffinityCache)
			optionRoute.POST("/rest_model_ratio", controller.ResetModelRatio)
			optionRoute.POST("/migrate_console_setting", controller.MigrateConsoleSetting) // 用于迁移检测的旧键，下个版本会删除
			optionRoute.POST("/waffo-pancake/catalog", controller.ListWaffoPancakeCatalog)
			optionRoute.POST("/waffo-pancake/pair", controller.CreateWaffoPancakePair)
			optionRoute.POST("/waffo-pancake/save", controller.SaveWaffoPancake)
			optionRoute.POST("/waffo-pancake/subscription-product", controller.CreateWaffoPancakeSubscriptionProduct)
			optionRoute.GET("/waffo-pancake/subscription-product-options", controller.ListWaffoPancakeSubscriptionProductOptions)
		}

		// Custom OAuth provider management (root only)
		customOAuthRoute := apiRouter.Group("/custom-oauth-provider")
		customOAuthRoute.Use(middleware.RootAuth())
		{
			customOAuthRoute.POST("/discovery", controller.FetchCustomOAuthDiscovery)
			customOAuthRoute.GET("/", controller.GetCustomOAuthProviders)
			customOAuthRoute.GET("/:id", controller.GetCustomOAuthProvider)
			customOAuthRoute.POST("/", controller.CreateCustomOAuthProvider)
			customOAuthRoute.PUT("/:id", controller.UpdateCustomOAuthProvider)
			customOAuthRoute.DELETE("/:id", controller.DeleteCustomOAuthProvider)
		}
		performanceRoute := apiRouter.Group("/performance")
		performanceRoute.Use(middleware.RootAuth())
		{
			performanceRoute.GET("/stats", controller.GetPerformanceStats)
			performanceRoute.DELETE("/disk_cache", controller.ClearDiskCache)
			performanceRoute.POST("/reset_stats", controller.ResetPerformanceStats)
			performanceRoute.POST("/gc", controller.ForceGC)
			performanceRoute.GET("/logs", controller.GetLogFiles)
			performanceRoute.DELETE("/logs", controller.CleanupLogFiles)
		}
		ratioSyncRoute := apiRouter.Group("/ratio_sync")
		ratioSyncRoute.Use(middleware.RootAuth())
		{
			ratioSyncRoute.GET("/channels", controller.GetSyncableChannels)
			ratioSyncRoute.POST("/fetch", controller.FetchUpstreamRatios)
		}
		telegramPushRoute := apiRouter.Group("/telegram_push")
		telegramPushRoute.Use(middleware.RootAuth())
		{
			telegramPushRoute.GET("/settings", controller.GetTelegramPushSettings)
			telegramPushRoute.PUT("/settings", controller.UpdateTelegramPushSettings)
			telegramPushRoute.POST("/test", controller.TestTelegramPush)
			telegramPushRoute.POST("/announcements", controller.PushAnnouncementToTelegram)
			telegramPushRoute.GET("/records", controller.ListTelegramPushRecords)
			telegramPushRoute.POST("/records/:id/retry", controller.RetryTelegramPushRecord)
		}
		channelRoute := apiRouter.Group("/channel")
		channelRoute.Use(middleware.AdminAuth())
		{
			channelRoute.GET("/", controller.GetAllChannels)
			channelRoute.GET("/search", controller.SearchChannels)
			channelRoute.GET("/models", controller.ChannelListModels)
			channelRoute.GET("/models_enabled", controller.EnabledListModels)
			channelRoute.GET("/upstream_updates/task/:task_id", controller.GetChannelUpstreamModelUpdateTask)
			channelRoute.GET("/:id", controller.GetChannel)
			channelRoute.POST("/:id/key", middleware.RootAuth(), middleware.UserCriticalRateLimit("CT:channel-key"), middleware.DisableCache(), middleware.SecureVerificationRequired(), controller.GetChannelKey)
			channelRoute.GET("/test", controller.TestAllChannels)
			channelRoute.GET("/test/:id", controller.TestChannel)
			channelRoute.GET("/update_balance", controller.UpdateAllChannelsBalance)
			channelRoute.GET("/update_balance/:id", controller.UpdateChannelBalance)
			channelRoute.POST("/", controller.AddChannel)
			channelRoute.PUT("/", controller.UpdateChannel)
			channelRoute.DELETE("/disabled", controller.DeleteDisabledChannel)
			channelRoute.POST("/tag/disabled", controller.DisableTagChannels)
			channelRoute.POST("/tag/enabled", controller.EnableTagChannels)
			channelRoute.PUT("/tag", controller.EditTagChannels)
			channelRoute.DELETE("/:id", controller.DeleteChannel)
			channelRoute.POST("/batch", controller.DeleteChannelBatch)
			channelRoute.POST("/fix", controller.FixChannelsAbilities)
			channelRoute.GET("/fetch_models/:id", controller.FetchUpstreamModels)
			channelRoute.POST("/fetch_models", middleware.RootAuth(), controller.FetchModels)
			channelRoute.POST("/codex/oauth/start", controller.StartCodexOAuth)
			channelRoute.POST("/codex/oauth/complete", controller.CompleteCodexOAuth)
			channelRoute.POST("/:id/codex/oauth/start", controller.StartCodexOAuthForChannel)
			channelRoute.POST("/:id/codex/oauth/complete", controller.CompleteCodexOAuthForChannel)
			channelRoute.POST("/:id/codex/refresh", controller.RefreshCodexChannelCredential)
			channelRoute.GET("/:id/codex/usage", controller.GetCodexChannelUsage)
			channelRoute.GET("/:id/codex/usage/reset-credits", controller.GetCodexChannelRateLimitResetCredits)
			channelRoute.POST("/:id/codex/usage/reset", controller.ResetCodexChannelUsage)
			channelRoute.POST("/ollama/pull", controller.OllamaPullModel)
			channelRoute.POST("/ollama/pull/stream", controller.OllamaPullModelStream)
			channelRoute.DELETE("/ollama/delete", controller.OllamaDeleteModel)
			channelRoute.GET("/ollama/version/:id", controller.OllamaVersion)
			channelRoute.POST("/batch/tag", controller.BatchSetChannelTag)
			channelRoute.GET("/tag/models", controller.GetTagModels)
			channelRoute.POST("/copy/:id", controller.CopyChannel)
			channelRoute.POST("/multi_key/manage", controller.ManageMultiKeys)
			channelRoute.POST("/upstream_updates/apply", controller.ApplyChannelUpstreamModelUpdates)
			channelRoute.POST("/upstream_updates/apply_all", controller.ApplyAllChannelUpstreamModelUpdates)
			channelRoute.POST("/upstream_updates/detect", controller.DetectChannelUpstreamModelUpdates)
			channelRoute.POST("/upstream_updates/detect_all", controller.DetectAllChannelUpstreamModelUpdates)
		}
		tokenRoute := apiRouter.Group("/token")
		tokenRoute.Use(middleware.UserAuth())
		{
			tokenRoute.GET("/", controller.GetAllTokens)
			tokenRoute.GET("/search", middleware.SearchRateLimit(), controller.SearchTokens)
			tokenRoute.POST("/usage/batch", controller.GetTokenUsageStatsBatch)
			tokenRoute.GET("/:id", controller.GetToken)
			tokenRoute.GET("/:id/usage", controller.GetTokenUsageStats)
			tokenRoute.POST("/:id/key", middleware.UserCriticalRateLimit("CT:token-key"), middleware.DisableCache(), controller.GetTokenKey)
			tokenRoute.POST("/:id/usage/reset", controller.ResetTokenUsageStats)
			tokenRoute.POST("/", controller.AddToken)
			tokenRoute.PUT("/", controller.UpdateToken)
			tokenRoute.DELETE("/:id", controller.DeleteToken)
			tokenRoute.POST("/batch", controller.DeleteTokenBatch)
			tokenRoute.POST("/batch/keys", middleware.UserCriticalRateLimit("CT:token-key-batch"), middleware.DisableCache(), controller.GetTokenKeysBatch)
		}

		usageRoute := apiRouter.Group("/usage")
		usageRoute.Use(middleware.CORS())
		{
			tokenUsageRoute := usageRoute.Group("/token")
			tokenUsageRoute.Use(middleware.TokenAuthReadOnly(), middleware.AuthenticatedCriticalRateLimit("CT:usage-token"))
			{
				tokenUsageRoute.GET("/", controller.GetTokenUsage)
			}
		}

		redemptionRoute := apiRouter.Group("/redemption")
		redemptionRoute.Use(middleware.AdminAuth())
		{
			redemptionRoute.GET("/", controller.GetAllRedemptions)
			redemptionRoute.GET("/search", controller.SearchRedemptions)
			redemptionRoute.GET("/:id", controller.GetRedemption)
			redemptionRoute.POST("/", controller.AddRedemption)
			redemptionRoute.PUT("/", controller.UpdateRedemption)
			redemptionRoute.DELETE("/invalid", controller.DeleteInvalidRedemption)
			redemptionRoute.DELETE("/:id", controller.DeleteRedemption)
		}
		logRoute := apiRouter.Group("/log")
		logRoute.GET("/", middleware.AdminAuth(), controller.GetAllLogs)
		logRoute.DELETE("/", middleware.AdminAuth(), controller.DeleteHistoryLogs)
		logRoute.GET("/stat", middleware.AdminAuth(), controller.GetLogsStat)
		logRoute.GET("/self/stat", middleware.UserAuth(), controller.GetLogsSelfStat)
		logRoute.GET("/channel_affinity_usage_cache", middleware.AdminAuth(), controller.GetChannelAffinityUsageCacheStats)
		logRoute.GET("/search", middleware.AdminAuth(), controller.SearchAllLogs)
		logRoute.GET("/self", middleware.UserAuth(), controller.GetUserLogs)
		logRoute.GET("/self/search", middleware.UserAuth(), middleware.SearchRateLimit(), controller.SearchUserLogs)

		dataRoute := apiRouter.Group("/data")
		dataRoute.GET("/", middleware.AdminAuth(), controller.GetAllQuotaDates)
		dataRoute.GET("/users", middleware.AdminAuth(), controller.GetQuotaDatesByUser)
		dataRoute.GET("/self", middleware.UserAuth(), controller.GetUserQuotaDates)

		logRoute.Use(middleware.CORS())
		{
			logRoute.GET("/token", middleware.TokenAuthReadOnly(), middleware.AuthenticatedCriticalRateLimit("CT:log-token"), controller.GetLogByKey)
		}
		groupRoute := apiRouter.Group("/group")
		groupRoute.Use(middleware.AdminAuth())
		{
			groupRoute.GET("/", controller.GetGroups)
		}

		prefillGroupRoute := apiRouter.Group("/prefill_group")
		prefillGroupRoute.Use(middleware.AdminAuth())
		{
			prefillGroupRoute.GET("/", controller.GetPrefillGroups)
			prefillGroupRoute.POST("/", controller.CreatePrefillGroup)
			prefillGroupRoute.PUT("/", controller.UpdatePrefillGroup)
			prefillGroupRoute.DELETE("/:id", controller.DeletePrefillGroup)
		}

		mjRoute := apiRouter.Group("/mj")
		mjRoute.GET("/self", middleware.UserAuth(), controller.GetUserMidjourney)
		mjRoute.GET("/", middleware.AdminAuth(), controller.GetAllMidjourney)

		taskRoute := apiRouter.Group("/task")
		{
			taskRoute.GET("/self", middleware.UserAuth(), controller.GetUserTask)
			taskRoute.GET("/", middleware.AdminAuth(), controller.GetAllTask)
		}

		systemTaskRoute := apiRouter.Group("/system-task")
		systemTaskRoute.Use(middleware.RootAuth())
		{
			systemTaskRoute.POST("/log-cleanup", controller.CreateLogCleanupSystemTask)
			systemTaskRoute.GET("/list", controller.ListSystemTasks)
			systemTaskRoute.GET("/current", controller.GetCurrentSystemTask)
			systemTaskRoute.GET("/:task_id", controller.GetSystemTask)
		}

		systemInfoRoute := apiRouter.Group("/system-info")
		systemInfoRoute.Use(middleware.RootAuth())
		{
			systemInfoRoute.GET("/instances", controller.ListSystemInstances)
		}

		vendorRoute := apiRouter.Group("/vendors")
		vendorRoute.Use(middleware.AdminAuth())
		{
			vendorRoute.GET("/", controller.GetAllVendors)
			vendorRoute.GET("/search", controller.SearchVendors)
			vendorRoute.GET("/:id", controller.GetVendorMeta)
			vendorRoute.POST("/", controller.CreateVendorMeta)
			vendorRoute.PUT("/", controller.UpdateVendorMeta)
			vendorRoute.DELETE("/:id", controller.DeleteVendorMeta)
		}

		modelsRoute := apiRouter.Group("/models")
		modelsRoute.Use(middleware.AdminAuth())
		{
			modelsRoute.GET("/sync_upstream/preview", controller.SyncUpstreamPreview)
			modelsRoute.POST("/sync_upstream", controller.SyncUpstreamModels)
			modelsRoute.GET("/missing", controller.GetMissingModels)
			modelsRoute.GET("/", controller.GetAllModelsMeta)
			modelsRoute.GET("/search", controller.SearchModelsMeta)
			modelsRoute.GET("/:id", controller.GetModelMeta)
			modelsRoute.POST("/", controller.CreateModelMeta)
			modelsRoute.PUT("/", controller.UpdateModelMeta)
			modelsRoute.DELETE("/:id", controller.DeleteModelMeta)
		}

		// Deployments (model deployment management)
		deploymentsRoute := apiRouter.Group("/deployments")
		deploymentsRoute.Use(middleware.AdminAuth())
		{
			deploymentsRoute.GET("/settings", controller.GetModelDeploymentSettings)
			deploymentsRoute.POST("/settings/test-connection", controller.TestIoNetConnection)
			deploymentsRoute.GET("/", controller.GetAllDeployments)
			deploymentsRoute.GET("/search", controller.SearchDeployments)
			deploymentsRoute.POST("/test-connection", controller.TestIoNetConnection)
			deploymentsRoute.GET("/hardware-types", controller.GetHardwareTypes)
			deploymentsRoute.GET("/locations", controller.GetLocations)
			deploymentsRoute.GET("/available-replicas", controller.GetAvailableReplicas)
			deploymentsRoute.POST("/price-estimation", controller.GetPriceEstimation)
			deploymentsRoute.GET("/check-name", controller.CheckClusterNameAvailability)
			deploymentsRoute.POST("/", controller.CreateDeployment)

			deploymentsRoute.GET("/:id", controller.GetDeployment)
			deploymentsRoute.GET("/:id/logs", controller.GetDeploymentLogs)
			deploymentsRoute.GET("/:id/containers", controller.ListDeploymentContainers)
			deploymentsRoute.GET("/:id/containers/:container_id", controller.GetContainerDetails)
			deploymentsRoute.PUT("/:id", controller.UpdateDeployment)
			deploymentsRoute.PUT("/:id/name", controller.UpdateDeploymentName)
			deploymentsRoute.POST("/:id/extend", controller.ExtendDeployment)
			deploymentsRoute.DELETE("/:id", controller.DeleteDeployment)
		}
	}
}
