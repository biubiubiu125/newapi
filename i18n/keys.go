package i18n

// Message keys for i18n translations
// Use these constants instead of hardcoded strings

// Common error messages
const (
	MsgInvalidParams     = "common.invalid_params"
	MsgDatabaseError     = "common.database_error"
	MsgRetryLater        = "common.retry_later"
	MsgGenerateFailed    = "common.generate_failed"
	MsgNotFound          = "common.not_found"
	MsgUnauthorized      = "common.unauthorized"
	MsgForbidden         = "common.forbidden"
	MsgInvalidId         = "common.invalid_id"
	MsgIdEmpty           = "common.id_empty"
	MsgFeatureDisabled   = "common.feature_disabled"
	MsgOperationSuccess  = "common.operation_success"
	MsgOperationFailed   = "common.operation_failed"
	MsgUpdateSuccess     = "common.update_success"
	MsgUpdateFailed      = "common.update_failed"
	MsgCreateSuccess     = "common.create_success"
	MsgCreateFailed      = "common.create_failed"
	MsgDeleteSuccess     = "common.delete_success"
	MsgDeleteFailed      = "common.delete_failed"
	MsgAlreadyExists     = "common.already_exists"
	MsgNameCannotBeEmpty = "common.name_cannot_be_empty"
	MsgBatchTooMany      = "common.batch_too_many"
)

// Auth middleware messages
const (
	MsgAuthNotLoggedIn           = "auth.not_logged_in"
	MsgAuthAccessTokenInvalid    = "auth.access_token_invalid"
	MsgAuthUserInfoInvalid       = "auth.user_info_invalid"
	MsgAuthUserIdNotProvided     = "auth.user_id_not_provided"
	MsgAuthUserIdFormatError     = "auth.user_id_format_error"
	MsgAuthUserIdMismatch        = "auth.user_id_mismatch"
	MsgAuthUserBanned            = "auth.user_banned"
	MsgAuthInsufficientPrivilege = "auth.insufficient_privilege"
	MsgAuthLoginRequired         = "auth.login_required"
)

// Token related messages
const (
	MsgTokenNameTooLong          = "token.name_too_long"
	MsgTokenQuotaNegative        = "token.quota_negative"
	MsgTokenQuotaExceedMax       = "token.quota_exceed_max"
	MsgTokenGenerateFailed       = "token.generate_failed"
	MsgTokenGetInfoFailed        = "token.get_info_failed"
	MsgTokenExpiredCannotEnable  = "token.expired_cannot_enable"
	MsgTokenExhaustedCannotEable = "token.exhausted_cannot_enable"
	MsgTokenInvalid              = "token.invalid"
	MsgTokenNotProvided          = "token.not_provided"
	MsgTokenExpired              = "token.expired"
	MsgTokenExhausted            = "token.exhausted"
	MsgTokenStatusUnavailable    = "token.status_unavailable"
	MsgTokenDbError              = "token.db_error"
	MsgTokenAutoGroupsTooMany    = "token.auto_groups_too_many"
	MsgTokenAutoGroupsDuplicate  = "token.auto_groups_duplicate"
	MsgTokenAutoGroupsInvalid    = "token.auto_groups_invalid"
)

// Redemption related messages
const (
	MsgRedemptionNameLength        = "redemption.name_length"
	MsgRedemptionQuotaPositive     = "redemption.quota_positive"
	MsgRedemptionCountPositive     = "redemption.count_positive"
	MsgRedemptionCountMax          = "redemption.count_max"
	MsgRedemptionCreateFailed      = "redemption.create_failed"
	MsgRedemptionInvalid           = "redemption.invalid"
	MsgRedemptionUsed              = "redemption.used"
	MsgRedemptionExpired           = "redemption.expired"
	MsgRedemptionFailed            = "redemption.failed"
	MsgRedemptionNotProvided       = "redemption.not_provided"
	MsgRedemptionExpireTimeInvalid = "redemption.expire_time_invalid"
)

// User related messages
const (
	MsgUserPasswordLoginDisabled     = "user.password_login_disabled"
	MsgUserRegisterDisabled          = "user.register_disabled"
	MsgUserPasswordRegisterDisabled  = "user.password_register_disabled"
	MsgUserUsernameOrPasswordEmpty   = "user.username_or_password_empty"
	MsgUserUsernameOrPasswordError   = "user.username_or_password_error"
	MsgUserEmailOrPasswordEmpty      = "user.email_or_password_empty"
	MsgUserExists                    = "user.exists"
	MsgUserSelfInviteNotAllowed      = "user.self_invite_not_allowed"
	MsgUserReferralCycleNotAllowed   = "user.referral_cycle_not_allowed"
	MsgUserNotExists                 = "user.not_exists"
	MsgUserDisabled                  = "user.disabled"
	MsgUserSessionSaveFailed         = "user.session_save_failed"
	MsgUserRequire2FA                = "user.require_2fa"
	MsgUserEmailVerificationRequired = "user.email_verification_required"
	MsgUserVerificationCodeError     = "user.verification_code_error"
	MsgUserEmailAlreadyTaken         = "user.email_already_taken"
	MsgUserPasswordUnset             = "user.password_unset"
	MsgUserPasswordResetLinkInvalid  = "user.password_reset_link_invalid"
	MsgUserInputInvalid              = "user.input_invalid"
	MsgUserUsernameInvalid           = "user.username_invalid"
	MsgUserUsernameTooLong           = "user.username_too_long"
	MsgUserEmailInvalid              = "user.email_invalid"
	MsgUserEmailTooLong              = "user.email_too_long"
	MsgUserPasswordLength            = "user.password_length"
	MsgUserDisplayNameTooLong        = "user.display_name_too_long"
	MsgUserRemarkTooLong             = "user.remark_too_long"
	MsgUserNoPermissionSameLevel     = "user.no_permission_same_level"
	MsgUserNoPermissionHigherLevel   = "user.no_permission_higher_level"
	MsgUserCannotCreateHigherLevel   = "user.cannot_create_higher_level"
	MsgUserCannotDeleteRootUser      = "user.cannot_delete_root_user"
	MsgUserCannotDisableRootUser     = "user.cannot_disable_root_user"
	MsgUserCannotDemoteRootUser      = "user.cannot_demote_root_user"
	MsgUserAlreadyAdmin              = "user.already_admin"
	MsgUserAlreadyCommon             = "user.already_common"
	MsgUserAdminCannotPromote        = "user.admin_cannot_promote"
	MsgUserOriginalPasswordError     = "user.original_password_error"
	MsgUserInviteQuotaInsufficient   = "user.invite_quota_insufficient"
	MsgUserTransferQuotaMinimum      = "user.transfer_quota_minimum"
	MsgUserTransferSuccess           = "user.transfer_success"
	MsgUserTransferFailed            = "user.transfer_failed"
	MsgUserTopUpProcessing           = "user.topup_processing"
	MsgUserRegisterFailed            = "user.register_failed"
	MsgUserDefaultTokenFailed        = "user.default_token_failed"
	MsgUserEmailEmpty                = "user.email_empty"
	MsgUserGitHubIdEmpty             = "user.github_id_empty"
	MsgUserDiscordIdEmpty            = "user.discord_id_empty"
	MsgUserOidcIdEmpty               = "user.oidc_id_empty"
	MsgUserWeChatIdEmpty             = "user.wechat_id_empty"
	MsgUserTelegramIdEmpty           = "user.telegram_id_empty"
	MsgUserTelegramNotBound          = "user.telegram_not_bound"
	MsgUserLinuxDOIdEmpty            = "user.linux_do_id_empty"
	MsgUserQuotaChangeZero           = "user.quota_change_zero"
)

// Quota related messages
const (
	MsgQuotaNegative        = "quota.negative"
	MsgQuotaExceedMax       = "quota.exceed_max"
	MsgQuotaInsufficient    = "quota.insufficient"
	MsgQuotaWarningInvalid  = "quota.warning_invalid"
	MsgQuotaThresholdGtZero = "quota.threshold_gt_zero"
)

// Subscription related messages
const (
	MsgSubscriptionNotEnabled       = "subscription.not_enabled"
	MsgSubscriptionTitleEmpty       = "subscription.title_empty"
	MsgSubscriptionPriceNegative    = "subscription.price_negative"
	MsgSubscriptionPriceMax         = "subscription.price_max"
	MsgSubscriptionPurchaseLimitNeg = "subscription.purchase_limit_negative"
	MsgSubscriptionQuotaNegative    = "subscription.quota_negative"
	MsgSubscriptionGroupNotExists   = "subscription.group_not_exists"
	MsgSubscriptionResetCycleGtZero = "subscription.reset_cycle_gt_zero"
	MsgSubscriptionPurchaseMax      = "subscription.purchase_max"
	MsgSubscriptionInvalidId        = "subscription.invalid_id"
	MsgSubscriptionInvalidUserId    = "subscription.invalid_user_id"
)

// Payment related messages
const (
	MsgPaymentNotConfigured      = "payment.not_configured"
	MsgPaymentMethodNotExists    = "payment.method_not_exists"
	MsgPaymentCallbackError      = "payment.callback_error"
	MsgPaymentCreateFailed       = "payment.create_failed"
	MsgPaymentStartFailed        = "payment.start_failed"
	MsgPaymentAmountTooLow       = "payment.amount_too_low"
	MsgPaymentStripeNotConfig    = "payment.stripe_not_configured"
	MsgPaymentWebhookNotConfig   = "payment.webhook_not_configured"
	MsgPaymentPriceIdNotConfig   = "payment.price_id_not_configured"
	MsgPaymentCreemNotConfig     = "payment.creem_not_configured"
	MsgPaymentComplianceRequired = "payment.compliance_required"
)

// Topup related messages
const (
	MsgTopupNotProvided    = "topup.not_provided"
	MsgTopupOrderNotExists = "topup.order_not_exists"
	MsgTopupOrderStatus    = "topup.order_status"
	MsgTopupFailed         = "topup.failed"
	MsgTopupInvalidQuota   = "topup.invalid_quota"
)

// Channel related messages
const (
	MsgChannelNotExists               = "channel.not_exists"
	MsgChannelIdFormatError           = "channel.id_format_error"
	MsgChannelNoAvailableKey          = "channel.no_available_key"
	MsgChannelGetListFailed           = "channel.get_list_failed"
	MsgChannelGetTagsFailed           = "channel.get_tags_failed"
	MsgChannelGetKeyFailed            = "channel.get_key_failed"
	MsgChannelGetOllamaFailed         = "channel.get_ollama_failed"
	MsgChannelQueryFailed             = "channel.query_failed"
	MsgChannelNoValidUpstream         = "channel.no_valid_upstream"
	MsgChannelUpstreamSaturated       = "channel.upstream_saturated"
	MsgChannelGetAvailableFailed      = "channel.get_available_failed"
	MsgChannelGetModelsFailed         = "channel.get_models_failed"
	MsgChannelGetInfoFailed           = "channel.get_info_failed"
	MsgChannelInvalidSettings         = "channel.invalid_settings"
	MsgChannelCloneFailed             = "channel.clone_failed"
	MsgChannelOllamaOnly              = "channel.ollama_only"
	MsgChannelGetOllamaVersionFailed  = "channel.get_ollama_version_failed"
	MsgChannelInvalidType             = "channel.invalid_type"
	MsgChannelCodexMultiKeyDraft      = "channel.codex_multi_key_draft"
	MsgChannelCodexKeyJSON            = "channel.codex_key_json"
	MsgChannelCodexKeyAccessToken     = "channel.codex_key_access_token"
	MsgChannelCodexKeyAccountID       = "channel.codex_key_account_id"
	MsgChannelEmpty                   = "channel.empty"
	MsgChannelSettingInvalid          = "channel.setting_invalid"
	MsgChannelModelNameTooLong        = "channel.model_name_too_long"
	MsgChannelSub2APIBaseURLRequired  = "channel.sub2api_base_url_required"
	MsgChannelNewAPIBaseURLRequired   = "channel.newapi_base_url_required"
	MsgChannelVertexRegionRequired    = "channel.vertex_region_required"
	MsgChannelVertexRegionJSON        = "channel.vertex_region_json"
	MsgChannelVertexRegionDefault     = "channel.vertex_region_default"
	MsgChannelTaskPluginKeyRequired   = "channel.task_plugin_key_required"
	MsgChannelTaskPluginNotRegistered = "channel.task_plugin_not_registered"
	MsgChannelGetTagCountFailed       = "channel.get_tag_count_failed"
	MsgChannelGetTagChannelsFailed    = "channel.get_tag_channels_failed"
	MsgChannelGetCountFailed          = "channel.get_count_failed"
	MsgChannelGetTypeCountsFailed     = "channel.get_type_counts_failed"
	MsgChannelGetSuccess              = "channel.get_success"
	MsgChannelRefreshCredentialFailed = "channel.refresh_credential_failed"
	MsgChannelRefreshed               = "channel.refreshed"
	MsgChannelVertexBatchJSON         = "channel.vertex_batch_json"
	MsgChannelVertexKeyEncodeFailed   = "channel.vertex_key_encode_failed"
	MsgChannelVertexKeysEmpty         = "channel.vertex_keys_empty"
	MsgChannelSettingNotJSON          = "channel.setting_not_json"
	MsgChannelUnsupportedAddMode      = "channel.unsupported_add_mode"
	MsgChannelTagEmpty                = "channel.tag_empty"
	MsgChannelParamOverrideJSON       = "channel.param_override_json"
	MsgChannelHeaderOverrideJSON      = "channel.header_override_json"
	MsgChannelMultiKeyModeInvalid     = "channel.multi_key_mode_invalid"
	MsgChannelAppendKeyParseFailed    = "channel.append_key_parse_failed"
	MsgChannelNotMultiKey             = "channel.not_multi_key"
	MsgChannelKeyDisableIndexRequired = "channel.key_disable_index_required"
	MsgChannelKeyEnableIndexRequired  = "channel.key_enable_index_required"
	MsgChannelKeyDeleteIndexRequired  = "channel.key_delete_index_required"
	MsgChannelKeyIndexOutOfRange      = "channel.key_index_out_of_range"
	MsgChannelKeyDisabled             = "channel.key_disabled"
	MsgChannelKeyEnabled              = "channel.key_enabled"
	MsgChannelKeysEnabled             = "channel.keys_enabled"
	MsgChannelNoDisableableKeys       = "channel.no_disableable_keys"
	MsgChannelKeysDisabled            = "channel.keys_disabled"
	MsgChannelCannotDeleteLastKey     = "channel.cannot_delete_last_key"
	MsgChannelKeyDeleted              = "channel.key_deleted"
	MsgChannelNoAutoDisabledKeys      = "channel.no_auto_disabled_keys"
	MsgChannelAutoDisabledKeysDeleted = "channel.auto_disabled_keys_deleted"
	MsgChannelUnsupportedOperation    = "channel.unsupported_operation"
	MsgChannelChannelAndModelRequired = "channel.channel_and_model_required"
	MsgChannelModelDeleted            = "channel.model_deleted"
	MsgChannelSensitiveWriteDenied    = "channel.sensitive_write_denied"
	MsgChannelOllamaModelEmpty        = "channel.ollama_model_empty"
	MsgChannelOllamaPullFailed        = "channel.ollama_pull_failed"
	MsgChannelOllamaPullStarted       = "channel.ollama_pull_started"
	MsgChannelOllamaPullSuccess       = "channel.ollama_pull_success"
	MsgChannelOllamaDeleteFailed      = "channel.ollama_delete_failed"
	MsgChannelOllamaDeleteSuccess     = "channel.ollama_delete_success"
)

// Model related messages
const (
	MsgModelNameEmpty     = "model.name_empty"
	MsgModelNameExists    = "model.name_exists"
	MsgModelIdMissing     = "model.id_missing"
	MsgModelGetListFailed = "model.get_list_failed"
	MsgModelGetFailed     = "model.get_failed"
	MsgModelResetSuccess  = "model.reset_success"
)

// Vendor related messages
const (
	MsgVendorNameEmpty  = "vendor.name_empty"
	MsgVendorNameExists = "vendor.name_exists"
	MsgVendorIdMissing  = "vendor.id_missing"
)

// Group related messages
const (
	MsgGroupNameTypeEmpty = "group.name_type_empty"
	MsgGroupNameExists    = "group.name_exists"
	MsgGroupIdMissing     = "group.id_missing"
)

// Checkin related messages
const (
	MsgCheckinDisabled     = "checkin.disabled"
	MsgCheckinAlreadyToday = "checkin.already_today"
	MsgCheckinFailed       = "checkin.failed"
	MsgCheckinQuotaFailed  = "checkin.quota_failed"
	MsgCheckinSuccess      = "checkin.success"
)

// Passkey related messages
const (
	MsgPasskeyCreateFailed          = "passkey.create_failed"
	MsgPasskeyLoginAbnormal         = "passkey.login_abnormal"
	MsgPasskeyUpdateFailed          = "passkey.update_failed"
	MsgPasskeyInvalidUserId         = "passkey.invalid_user_id"
	MsgPasskeyVerifyFailed          = "passkey.verify_failed"
	MsgPasskeyNotEnabled            = "passkey.not_enabled"
	MsgPasskeyNotBound              = "passkey.not_bound"
	MsgPasskeyAuthMethodUnsupported = "passkey.auth_method_unsupported"
	MsgPasskeyUnsupportedScope      = "passkey.unsupported_scope"
	MsgPasskeyRegisterSuccess       = "passkey.register_success"
	MsgPasskeyUnbound               = "passkey.unbound"
	MsgPasskeyVerifySuccess         = "passkey.verify_success"
	MsgPasskeyResetSuccess          = "passkey.reset_success"
	MsgPasskeyParamsIncomplete      = "passkey.params_incomplete"
	MsgPasskeyCredentialNotFound    = "passkey.credential_not_found"
	MsgPasskeyUserInfoFailed        = "passkey.user_info_failed"
	MsgPasskeyHandleMismatch        = "passkey.handle_mismatch"
	MsgPasskeyInvalidVerifyRequest  = "passkey.invalid_verify_request"
	MsgPasskeySessionExpired        = "passkey.session_expired"
	MsgPasskeySessionEmpty          = "passkey.session_empty"
	MsgPasskeySettingsMissing       = "passkey.settings_missing"
	MsgPasskeyInsecureOrigin        = "passkey.insecure_origin"
	MsgPasskeyHTTPSRequired         = "passkey.https_required"
	MsgPasskeyOriginUndetermined    = "passkey.origin_undetermined"
	MsgPasskeyOriginParseFailed     = "passkey.origin_parse_failed"
	MsgPasskeyRPIDOriginMissing     = "passkey.rpid_origin_missing"
)

// 2FA related messages
const (
	MsgTwoFANotEnabled    = "twofa.not_enabled"
	MsgTwoFAUserIdEmpty   = "twofa.user_id_empty"
	MsgTwoFAUserIdFormat  = "twofa.user_id_format"
	MsgTwoFAAlreadyExists = "twofa.already_exists"
	MsgTwoFARecordIdEmpty = "twofa.record_id_empty"
	MsgTwoFACodeInvalid   = "twofa.code_invalid"
	MsgTwoFACodeLength    = "twofa.code_length"
	MsgTwoFACodeDigits    = "twofa.code_digits"
)

const (
	MsgSMTPFromInvalid         = "smtp.from_invalid"
	MsgSMTPStartTLSUnsupported = "smtp.starttls_unsupported"
	MsgSMTPNotConfigured       = "smtp.not_configured"
)

// Rate limit related messages
const (
	MsgRateLimitReached      = "rate_limit.reached"
	MsgRateLimitTotalReached = "rate_limit.total_reached"
)

// Setting related messages
const (
	MsgSettingInvalidType      = "setting.invalid_type"
	MsgSettingWebhookEmpty     = "setting.webhook_empty"
	MsgSettingWebhookInvalid   = "setting.webhook_invalid"
	MsgSettingEmailInvalid     = "setting.email_invalid"
	MsgSettingBarkUrlEmpty     = "setting.bark_url_empty"
	MsgSettingBarkUrlInvalid   = "setting.bark_url_invalid"
	MsgSettingGotifyUrlEmpty   = "setting.gotify_url_empty"
	MsgSettingGotifyTokenEmpty = "setting.gotify_token_empty"
	MsgSettingGotifyUrlInvalid = "setting.gotify_url_invalid"
	MsgSettingUrlMustHttp      = "setting.url_must_http"
	MsgSettingSaved            = "setting.saved"
)

// Deployment related messages (io.net)
const (
	MsgDeploymentNotEnabled     = "deployment.not_enabled"
	MsgDeploymentIdRequired     = "deployment.id_required"
	MsgDeploymentContainerIdReq = "deployment.container_id_required"
	MsgDeploymentNameEmpty      = "deployment.name_empty"
	MsgDeploymentNameTaken      = "deployment.name_taken"
	MsgDeploymentHardwareIdReq  = "deployment.hardware_id_required"
	MsgDeploymentHardwareInvId  = "deployment.hardware_invalid_id"
	MsgDeploymentApiKeyRequired = "deployment.api_key_required"
	MsgDeploymentInvalidPayload = "deployment.invalid_payload"
	MsgDeploymentNotFound       = "deployment.not_found"
)

// Performance related messages
const (
	MsgPerfDiskCacheCleared = "performance.disk_cache_cleared"
	MsgPerfStatsReset       = "performance.stats_reset"
	MsgPerfGcExecuted       = "performance.gc_executed"
)

// Ability related messages
const (
	MsgAbilityDbCorrupted   = "ability.db_corrupted"
	MsgAbilityRepairRunning = "ability.repair_running"
)

// OAuth related messages
const (
	MsgOAuthInvalidCode             = "oauth.invalid_code"
	MsgOAuthGetUserErr              = "oauth.get_user_error"
	MsgOAuthAccountUsed             = "oauth.account_used"
	MsgOAuthUnknownProvider         = "oauth.unknown_provider"
	MsgOAuthStateInvalid            = "oauth.state_invalid"
	MsgOAuthNotEnabled              = "oauth.not_enabled"
	MsgOAuthUserDeleted             = "oauth.user_deleted"
	MsgOAuthUserBanned              = "oauth.user_banned"
	MsgOAuthBindSuccess             = "oauth.bind_success"
	MsgOAuthAlreadyBound            = "oauth.already_bound"
	MsgOAuthConnectFailed           = "oauth.connect_failed"
	MsgOAuthTokenFailed             = "oauth.token_failed"
	MsgOAuthUserInfoEmpty           = "oauth.user_info_empty"
	MsgOAuthTrustLevelLow           = "oauth.trust_level_low"
	MsgOAuthServerAddressRequired   = "oauth.server_address_required"
	MsgOAuthBindRequiresLogin       = "oauth.bind_requires_login"
	MsgOAuthWeChatBindUnsupported   = "oauth.wechat_bind_unsupported"
	MsgOAuthCredentialUsed          = "oauth.credential_used"
	MsgOAuthUnbindMethodUnsupported = "oauth.unbind_method_unsupported"
	MsgOAuthUnbindSuccess           = "oauth.unbind_success"
	MsgOAuthAccessDenied            = "oauth.access_denied"
	MsgOAuthAuthorizationCancelled  = "oauth.authorization_cancelled"
	MsgOAuthAuthorizationFailed     = "oauth.authorization_failed"
)

// Model layer error messages (for translation in controller)
const (
	MsgRedeemFailed          = "redeem.failed"
	MsgCreateDefaultTokenErr = "user.create_default_token_error"
	MsgUuidDuplicate         = "common.uuid_duplicate"
	MsgInvalidInput          = "common.invalid_input"
)

// Distributor related messages
const (
	MsgDistributorInvalidRequest               = "distributor.invalid_request"
	MsgDistributorInvalidChannelId             = "distributor.invalid_channel_id"
	MsgDistributorChannelDisabled              = "distributor.channel_disabled"
	MsgDistributorAffinityChannelDisabled      = "distributor.affinity_channel_disabled"
	MsgDistributorTokenNoModelAccess           = "distributor.token_no_model_access"
	MsgDistributorTokenModelForbidden          = "distributor.token_model_forbidden"
	MsgDistributorModelNameRequired            = "distributor.model_name_required"
	MsgDistributorInvalidPlayground            = "distributor.invalid_playground_request"
	MsgDistributorGroupAccessDenied            = "distributor.group_access_denied"
	MsgDistributorGetChannelFailed             = "distributor.get_channel_failed"
	MsgDistributorNoAvailableChannel           = "distributor.no_available_channel"
	MsgDistributorNoAvailableChannelTaskPlugin = "distributor.no_available_channel_task_plugin"
	MsgDistributorInvalidMidjourney            = "distributor.invalid_midjourney_request"
	MsgDistributorInvalidParseModel            = "distributor.invalid_request_parse_model"
)

// Custom OAuth provider related messages
const (
	MsgCustomOAuthNotFound               = "custom_oauth.not_found"
	MsgCustomOAuthSlugEmpty              = "custom_oauth.slug_empty"
	MsgCustomOAuthSlugExists             = "custom_oauth.slug_exists"
	MsgCustomOAuthNameEmpty              = "custom_oauth.name_empty"
	MsgCustomOAuthHasBindings            = "custom_oauth.has_bindings"
	MsgCustomOAuthBindingNotFound        = "custom_oauth.binding_not_found"
	MsgCustomOAuthProviderIdInvalid      = "custom_oauth.provider_id_field_invalid"
	MsgCustomOAuthSlugConflictsBuiltin   = "custom_oauth.slug_conflicts_builtin"
	MsgCustomOAuthDiscoveryURLRequired   = "custom_oauth.discovery_url_required"
	MsgCustomOAuthDiscoveryURLInvalid    = "custom_oauth.discovery_url_invalid"
	MsgCustomOAuthInvalidRequest         = "custom_oauth.invalid_request"
	MsgCustomOAuthDiscoveryRequestFailed = "custom_oauth.discovery_request_failed"
	MsgCustomOAuthDiscoveryFetchFailed   = "custom_oauth.discovery_fetch_failed"
	MsgCustomOAuthDiscoveryParseFailed   = "custom_oauth.discovery_parse_failed"
	MsgCustomOAuthInvalidProviderId      = "custom_oauth.invalid_provider_id"
)

// Email related messages
const (
	MsgEmailVerifySubject    = "email.verify_subject"
	MsgEmailVerifyContent    = "email.verify_content"
	MsgEmailResetSubject     = "email.reset_subject"
	MsgEmailResetContent     = "email.reset_content"
	MsgEmailDomainNotAllowed = "email.domain_not_allowed"
	MsgEmailAliasRestricted  = "email.alias_restricted"
	MsgEmailSendFailed       = "email.send_failed"
)

// Status related messages
const (
	MsgStatusDBFailed = "status.db_failed"
	MsgStatusRunning  = "status.running"
)

// Protocol envelope messages. Always render through ProtocolMessage (English catalog).
const (
	MsgProtocolAccessTokenUnsupported        = "protocol.access_token_unsupported"
	MsgProtocolImageTaskNGtOne               = "protocol.image_task_n_gt_one"
	MsgProtocolChannelRetryFailed            = "protocol.channel_retry_failed"
	MsgProtocolChannelRetryMissing           = "protocol.channel_retry_missing"
	MsgProtocolChannelMissing                = "protocol.channel_missing"
	MsgProtocolGroupSaturatedUpgrade         = "protocol.group_saturated_upgrade"
	MsgProtocolInsufficientUserQuota         = "protocol.insufficient_user_quota"
	MsgProtocolInsufficientSubscriptionQuota = "protocol.insufficient_subscription_quota"
	MsgProtocolModelPriceNotConfigured       = "protocol.model_price_not_configured"
	MsgProtocolModelPriceNotConfiguredAdmin  = "protocol.model_price_not_configured_admin"
	MsgProtocolFileTooLarge                  = "protocol.file_too_large"
	MsgProtocolPreConsumeFailed              = "protocol.pre_consume_failed"
)

// Setup wizard messages
const (
	MsgSetupAlreadyInitialized   = "setup.already_initialized"
	MsgSetupInvalidRequest       = "setup.invalid_request"
	MsgSetupPasswordMismatch     = "setup.password_mismatch"
	MsgSetupPasswordTooShort     = "setup.password_too_short"
	MsgSetupUsernameTooLong      = "setup.username_too_long"
	MsgSetupSystemError          = "setup.system_error"
	MsgSetupCreateAdminFailed    = "setup.create_admin_failed"
	MsgSetupRootMissing          = "setup.root_missing"
	MsgSetupUpdatePasswordFailed = "setup.update_password_failed"
	MsgSetupSaveSelfUseFailed    = "setup.save_self_use_failed"
	MsgSetupSaveDemoFailed       = "setup.save_demo_failed"
	MsgSetupInitFailed           = "setup.init_failed"
	MsgSetupSuccess              = "setup.success"
)

// Console session, ticket, turnstile, log, quota, token group, extra subscription/user messages
const (
	MsgAuthFlowInvalid               = "auth.flow_invalid"
	MsgAuthFlowExpired               = "auth.flow_expired"
	MsgAuthFlowConsumed              = "auth.flow_consumed"
	MsgAuthSessionRequired           = "auth.session_required"
	MsgAuthSessionIdRequired         = "auth.session_id_required"
	MsgAuthSessionNotFound           = "auth.session_not_found"
	MsgAuthSessionLimit              = "auth.session_limit"
	MsgAuthSessionIssuanceLimit      = "auth.session_issuance_limit"
	MsgAuthSessionMismatch           = "auth.session_mismatch"
	MsgAuthRefreshRace               = "auth.refresh_race"
	MsgAuthTokenExpired              = "auth.token_expired"
	MsgAuthSessionRevoked            = "auth.session_revoked"
	MsgAuthRotationBundleEmpty       = "auth.rotation_bundle_empty"
	MsgPaymentConfirmRequired        = "payment.confirm_required"
	MsgUserPasswordMethodUnsupported = "user.password_method_unsupported"
	MsgUserEmailMethodUnsupported    = "user.email_method_unsupported"
	MsgTicketTitleInvalid            = "ticket.title_invalid"
	MsgTicketContentEmpty            = "ticket.content_empty"
	MsgTicketCategoryInvalid         = "ticket.category_invalid"
	MsgTicketPriorityInvalid         = "ticket.priority_invalid"
	MsgTurnstileTokenEmpty           = "turnstile.token_empty"
	MsgTurnstileVerifyFailed         = "turnstile.verify_failed"
	MsgTurnstileNetworkFailed        = "turnstile.network_failed"
	MsgTurnstileDecodeFailed         = "turnstile.decode_failed"
	MsgLogDeprecated                 = "log.deprecated"
	MsgQuotaRangeExceedsMonth        = "quota.range_exceeds_month"
	MsgTokenGroupRequired            = "token.group_required"
	MsgTokenGroupForbidden           = "token.group_forbidden"
	MsgTokenGroupDeprecated          = "token.group_deprecated"
	MsgTokenClientIPUnparseable      = "token.client_ip_unparseable"
	MsgTokenClientIPNotAllowed       = "token.client_ip_not_allowed"
	MsgSubscriptionInvalidCurrency   = "subscription.invalid_currency"
	MsgSubscriptionInvalidPrice      = "subscription.invalid_price"
	MsgSubscriptionDowngradeGroup    = "subscription.downgrade_group_not_exists"
	MsgSubscriptionGrantGroup        = "subscription.grant_group_not_exists"
	MsgSecurePasskeyMustVerifyFlow   = "secure.passkey_must_verify_flow"
)
