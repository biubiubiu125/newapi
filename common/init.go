package common

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
)

var (
	Port         = flag.Int("port", 3000, "the listening port")
	PrintVersion = flag.Bool("version", false, "print version and exit")
	PrintHelp    = flag.Bool("help", false, "print help and exit")
	LogDir       = flag.String("log-dir", "./logs", "specify the log directory")
)

func printHelp() {
	fmt.Println("NewAPI(Based OneAPI) " + Version + " - The next-generation LLM gateway and AI asset management system supports multiple languages.")
	fmt.Println("Original Project: OneAPI by JustSong - https://github.com/songquanpeng/one-api")
	fmt.Println("Maintainer: QuantumNous - https://github.com/QuantumNous/new-api")
	fmt.Println("Usage: newapi [--port <port>] [--log-dir <log directory>] [--version] [--help]")
}

func InitEnv() {
	flag.Parse()

	envVersion := os.Getenv("VERSION")
	if envVersion != "" {
		Version = envVersion
	}

	if *PrintVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	if *PrintHelp {
		printHelp()
		os.Exit(0)
	}

	if os.Getenv("SESSION_SECRET") != "" {
		ss := os.Getenv("SESSION_SECRET")
		if isInsecureSecretPlaceholder(ss) {
			log.Println("WARNING: SESSION_SECRET is set to an insecure placeholder, please change it to a random string.")
			log.Println("警告：SESSION_SECRET 被设置为不安全的占位值，请修改为随机字符串。")
			log.Fatal("Please set SESSION_SECRET to a random string.")
		} else {
			SessionSecret = ss
		}
	}
	if os.Getenv("CRYPTO_SECRET") != "" {
		cs := os.Getenv("CRYPTO_SECRET")
		if isInsecureSecretPlaceholder(cs) {
			log.Println("WARNING: CRYPTO_SECRET is set to an insecure placeholder, please change it to a random string.")
			log.Println("警告：CRYPTO_SECRET 被设置为不安全的占位值，请修改为随机字符串。")
			log.Fatal("Please set CRYPTO_SECRET to a random string.")
		}
		CryptoSecret = cs
	} else {
		CryptoSecret = SessionSecret
	}
	if os.Getenv("REFERRAL_SIGNING_SECRET") != "" {
		ReferralSigningSecret = os.Getenv("REFERRAL_SIGNING_SECRET")
	} else {
		ReferralSigningSecret = CryptoSecret
	}
	if os.Getenv("REFERRAL_ASSET_SIGNING_SECRET") != "" {
		ReferralAssetSigningSecret = os.Getenv("REFERRAL_ASSET_SIGNING_SECRET")
	} else {
		ReferralAssetSigningSecret = ReferralSigningSecret
	}
	ReferralTestMode = GetEnvOrDefaultBool("REFERRAL_TEST_MODE", false)
	if err := InitSessionCookieSettings(); err != nil {
		log.Fatal(err)
	}
	if os.Getenv("SQLITE_PATH") != "" {
		SQLitePath = os.Getenv("SQLITE_PATH")
	}
	if *LogDir != "" {
		var err error
		*LogDir, err = filepath.Abs(*LogDir)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := os.Stat(*LogDir); os.IsNotExist(err) {
			err = os.Mkdir(*LogDir, 0777)
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	// Initialize variables from constants.go that were using environment variables
	DebugEnabled = os.Getenv("DEBUG") == "true"
	MemoryCacheEnabled = os.Getenv("MEMORY_CACHE_ENABLED") == "true"
	IsMasterNode = os.Getenv("NODE_TYPE") != "slave"
	initNodeNameIdentity()
	TLSInsecureSkipVerify = GetEnvOrDefaultBool("TLS_INSECURE_SKIP_VERIFY", false)
	if TLSInsecureSkipVerify {
		if tr, ok := http.DefaultTransport.(*http.Transport); ok && tr != nil {
			if tr.TLSClientConfig != nil {
				tr.TLSClientConfig.InsecureSkipVerify = true
			} else {
				tr.TLSClientConfig = InsecureTLSConfig
			}
		}
	}
	SMTPStartTLSEnabled = GetEnvOrDefaultBool("SMTP_STARTTLS_ENABLE", GetEnvOrDefaultBool("SMTP_STARTTLS_ENABLED", false))
	SMTPInsecureSkipVerify = GetEnvOrDefaultBool("SMTP_INSECURE_SKIP_VERIFY", GetEnvOrDefaultBool("SMTP_TLS_INSECURE_SKIP_VERIFY", false))

	// Parse requestInterval and set RequestInterval
	requestInterval, _ = strconv.Atoi(os.Getenv("POLLING_INTERVAL"))
	RequestInterval = time.Duration(requestInterval) * time.Second

	// Initialize variables with GetEnvOrDefault
	SyncFrequency = GetEnvOrDefault("SYNC_FREQUENCY", 60)
	BatchUpdateEnabled = GetEnvOrDefaultBool("BATCH_UPDATE_ENABLED", false)
	BatchUpdateInterval = GetEnvOrDefault("BATCH_UPDATE_INTERVAL", 5)
	TrustQuota = GetEnvOrDefault("TRUST_QUOTA", 0)
	RelayTimeout = GetEnvOrDefault("RELAY_TIMEOUT", 0)
	RelayIdleConnTimeout = GetEnvOrDefault("RELAY_IDLE_CONN_TIMEOUT", 90)
	RelayMaxIdleConns = GetEnvOrDefault("RELAY_MAX_IDLE_CONNS", 500)
	RelayMaxIdleConnsPerHost = GetEnvOrDefault("RELAY_MAX_IDLE_CONNS_PER_HOST", 100)

	// Initialize string variables with GetEnvOrDefaultString
	GeminiSafetySetting = GetEnvOrDefaultString("GEMINI_SAFETY_SETTING", "BLOCK_NONE")
	CohereSafetySetting = GetEnvOrDefaultString("COHERE_SAFETY_SETTING", "NONE")

	// Initialize rate limit variables
	GlobalApiRateLimitEnable = GetEnvOrDefaultBool("GLOBAL_API_RATE_LIMIT_ENABLE", true)
	GlobalApiRateLimitNum = GetEnvOrDefault("GLOBAL_API_RATE_LIMIT", 360)
	GlobalApiRateLimitDuration = int64(GetEnvOrDefault("GLOBAL_API_RATE_LIMIT_DURATION", 180))

	GlobalWebRateLimitEnable = GetEnvOrDefaultBool("GLOBAL_WEB_RATE_LIMIT_ENABLE", true)
	GlobalWebRateLimitNum = GetEnvOrDefault("GLOBAL_WEB_RATE_LIMIT", 120)
	GlobalWebRateLimitDuration = int64(GetEnvOrDefault("GLOBAL_WEB_RATE_LIMIT_DURATION", 180))

	CriticalRateLimitEnable = GetEnvOrDefaultBool("CRITICAL_RATE_LIMIT_ENABLE", true)
	CriticalRateLimitNum = GetEnvOrDefault("CRITICAL_RATE_LIMIT", 50)
	CriticalRateLimitDuration = int64(GetEnvOrDefault("CRITICAL_RATE_LIMIT_DURATION", 20*60))

	SearchRateLimitEnable = GetEnvOrDefaultBool("SEARCH_RATE_LIMIT_ENABLE", true)
	SearchRateLimitNum = GetEnvOrDefault("SEARCH_RATE_LIMIT", 10)
	SearchRateLimitDuration = int64(GetEnvOrDefault("SEARCH_RATE_LIMIT_DURATION", 60))
	InitTrustedProxyConfig()
	initConstantEnv()
}

func isInsecureSecretPlaceholder(secret string) bool {
	normalized := strings.ToLower(strings.TrimSpace(secret))
	return normalized == "random_string" ||
		strings.HasPrefix(normalized, "change_me") ||
		strings.Contains(normalized, "your_session_secret") ||
		strings.Contains(normalized, "your-random-session-secret")
}

func initConstantEnv() {
	constant.StreamingTimeout = GetEnvOrDefault("STREAMING_TIMEOUT", 300)
	constant.DifyDebug = GetEnvOrDefaultBool("DIFY_DEBUG", true)
	constant.MaxFileDownloadMB = GetEnvOrDefault("MAX_FILE_DOWNLOAD_MB", 64)
	constant.StreamScannerMaxBufferMB = GetEnvOrDefault("STREAM_SCANNER_MAX_BUFFER_MB", 128)
	// MaxRequestBodyMB 请求体最大大小（解压后），用于防止超大请求/zip bomb导致内存暴涨
	constant.MaxRequestBodyMB = GetEnvOrDefault("MAX_REQUEST_BODY_MB", 128)
	constant.AnonymousRequestBodyLimitKB = GetEnvOrDefault("ANONYMOUS_REQUEST_BODY_LIMIT_KB", 512)
	// ForceStreamOption 覆盖请求参数，强制返回usage信息
	constant.ForceStreamOption = GetEnvOrDefaultBool("FORCE_STREAM_OPTION", true)
	constant.CountToken = GetEnvOrDefaultBool("CountToken", true)
	constant.GetMediaToken = GetEnvOrDefaultBool("GET_MEDIA_TOKEN", true)
	constant.GetMediaTokenNotStream = GetEnvOrDefaultBool("GET_MEDIA_TOKEN_NOT_STREAM", false)
	constant.UpdateTask = GetEnvOrDefaultBool("UPDATE_TASK", true)
	constant.AzureDefaultAPIVersion = GetEnvOrDefaultString("AZURE_DEFAULT_API_VERSION", "2025-04-01-preview")
	constant.NotifyLimitCount = GetEnvOrDefault("NOTIFY_LIMIT_COUNT", 2)
	constant.NotificationLimitDurationMinute = GetEnvOrDefault("NOTIFICATION_LIMIT_DURATION_MINUTE", 10)
	// GenerateDefaultToken 是否生成初始令牌，默认关闭。
	constant.GenerateDefaultToken = GetEnvOrDefaultBool("GENERATE_DEFAULT_TOKEN", false)
	// 是否启用错误日志
	constant.ErrorLogEnabled = GetEnvOrDefaultBool("ERROR_LOG_ENABLED", false)
	// 任务轮询时查询的最大数量
	constant.TaskQueryLimit = GetEnvOrDefault("TASK_QUERY_LIMIT", 1000)
	// 异步任务超时时间（分钟），超过此时间未完成的任务将被标记为失败并退款。0 表示禁用。
	constant.TaskTimeoutMinutes = GetEnvOrDefault("TASK_TIMEOUT_MINUTES", 1440)
	constant.ImageTaskWorkerEnabled = GetEnvOrDefaultBool("IMAGE_TASK_WORKER_ENABLED", true)
	constant.ImageTaskWorkerIdleSeconds = GetEnvOrDefault("IMAGE_TASK_WORKER_IDLE_SECONDS", 5)
	constant.ImageTaskWorkerConcurrency = GetEnvOrDefault("IMAGE_TASK_WORKER_CONCURRENCY", 0)
	constant.ImageTaskChannelConcurrency = GetEnvOrDefault("IMAGE_TASK_CHANNEL_CONCURRENCY", 0)
	constant.ImageTaskBatchPollSize = GetEnvOrDefault("IMAGE_TASK_BATCH_POLL_SIZE", 20)
	constant.ImageTaskLeaseSeconds = GetEnvOrDefault("IMAGE_TASK_LEASE_SECONDS", 120)
	constant.ImageTaskResultRetentionMinutes = GetEnvOrDefault("IMAGE_TASK_RESULT_RETENTION_MINUTES", 720)
	constant.ImageTaskRequestBodyBase64MaxMB = GetEnvOrDefault("IMAGE_TASK_REQUEST_BODY_BASE64_MAX_MB", 16)
	constant.ImageTaskHTTPResponseMaxMB = GetEnvOrDefault("IMAGE_TASK_HTTP_RESPONSE_MAX_MB", 0)
	constant.ImageTaskFileCacheShared = GetEnvOrDefaultBool("IMAGE_TASK_FILE_CACHE_SHARED", false)
	constant.ImageTaskFileCacheSharedTrusted = GetEnvOrDefaultBool("IMAGE_TASK_FILE_CACHE_SHARED_TRUSTED", false)
	constant.ImageTaskLocalFileCacheAffinity = GetEnvOrDefaultBool("IMAGE_TASK_LOCAL_FILE_CACHE_AFFINITY", true)
	// 图片任务孤儿兜底：未提交上游且长期无节点认领的任务在该秒数后失败退款，0 表示禁用。
	constant.ImageTaskOrphanFailSeconds = GetEnvOrDefault("IMAGE_TASK_ORPHAN_FAIL_SECONDS", 1800)
	// 无法外置到受信共享缓存时，允许内联进数据库的结果上限（MB），0 表示不限制。
	constant.ImageTaskResultInlineMaxMB = GetEnvOrDefault("IMAGE_TASK_RESULT_INLINE_MAX_MB", 32)
	// 公开结果下载并发：全局与单 Token。0 表示对应维度不限制。
	// 大体积 b64_json 外置文件时用于避免单进程/单令牌同时把多份大结果整包读入内存。
	constant.ImageTaskResultDownloadConcurrency = GetEnvOrDefault("IMAGE_TASK_RESULT_DOWNLOAD_CONCURRENCY", 32)
	constant.ImageTaskResultDownloadTokenConcurrency = GetEnvOrDefault("IMAGE_TASK_RESULT_DOWNLOAD_TOKEN_CONCURRENCY", 4)
	// 图片任务状态/结果/ACK/取消接口的单令牌限流；异步 API 以轮询为前提，这里不能沿用
	// CRITICAL_RATE_LIMIT（默认 20 分钟 50 次）那种为敏感写操作准备的额度。0 表示不限流。
	constant.ImageTaskAccessRateLimitCount = GetEnvOrDefault("IMAGE_TASK_ACCESS_RATE_LIMIT", 600)
	constant.ImageTaskAccessRateLimitDurationSeconds = GetEnvOrDefault("IMAGE_TASK_ACCESS_RATE_LIMIT_DURATION", 60)
	constant.ImageTaskCreateRateLimitCount = GetEnvOrDefault("IMAGE_TASK_CREATE_RATE_LIMIT", 60)
	constant.ImageTaskCreateRateLimitDurationSeconds = GetEnvOrDefault("IMAGE_TASK_CREATE_RATE_LIMIT_DURATION", 60)
	constant.ImageTaskCreateMaxInFlight = GetEnvOrDefault("IMAGE_TASK_CREATE_MAX_IN_FLIGHT", 16)
	constant.ImageTaskCreateMaxReservedMB = GetEnvOrDefault("IMAGE_TASK_CREATE_MAX_RESERVED_MB", 1024)
	// 幂等预约行在绑定任务后不会自动消失，需要保留期回收，否则会随图片任务量无上限增长。
	constant.ImageTaskIdempotencyLockRetentionHours = GetEnvOrDefault("IMAGE_TASK_IDEMPOTENCY_LOCK_RETENTION_HOURS", 24*30)
	constant.SystemTaskHistoryRetentionHours = GetEnvOrDefault("SYSTEM_TASK_HISTORY_RETENTION_HOURS", 24*7)
	constant.TaskSettlementRecordRetentionHours = GetEnvOrDefault("TASK_SETTLEMENT_RECORD_RETENTION_HOURS", 24*30)

	soraPatchStr := GetEnvOrDefaultString("TASK_PRICE_PATCH", "")
	if soraPatchStr != "" {
		var taskPricePatches []string
		soraPatches := strings.Split(soraPatchStr, ",")
		for _, patch := range soraPatches {
			trimmedPatch := strings.TrimSpace(patch)
			if trimmedPatch != "" {
				taskPricePatches = append(taskPricePatches, trimmedPatch)
			}
		}
		constant.TaskPricePatches = taskPricePatches
	}

	// Initialize trusted redirect domains for URL validation
	trustedDomainsStr := GetEnvOrDefaultString("TRUSTED_REDIRECT_DOMAINS", "")
	var trustedDomains []string
	domains := strings.Split(trustedDomainsStr, ",")
	for _, domain := range domains {
		trimmedDomain := strings.TrimSpace(domain)
		if trimmedDomain != "" {
			// Normalize domain to lowercase
			trustedDomains = append(trustedDomains, strings.ToLower(trimmedDomain))
		}
	}
	constant.TrustedRedirectDomains = trustedDomains
}
