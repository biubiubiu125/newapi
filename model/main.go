package model

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var commonGroupCol string
var commonKeyCol string
var commonTrueVal string
var commonFalseVal string

var logKeyCol string
var logGroupCol string

const (
	defaultSQLMaxIdleConns = 25
	defaultSQLMaxOpenConns = 80
	defaultSQLMaxLifetime  = 60

	defaultDBStartupConnectTimeoutSeconds  = 60
	defaultDBStartupConnectRetryIntervalMs = 1000

	dropLegacyRiskTablesEnv = "DROP_LEGACY_RISK_TABLES"
)

var legacyConversationArtifactTables = []string{
	"conversation_snapshots",
	"conversation_export_tasks",
}

var legacyConversationArtifactOptions = []string{
	"ConversationSnapshotRetentionDays",
}

func initCol() {
	// init common column names
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		commonGroupCol = `"group"`
		commonKeyCol = `"key"`
		commonTrueVal = "true"
		commonFalseVal = "false"
	} else {
		commonGroupCol = "`group`"
		commonKeyCol = "`key`"
		commonTrueVal = "1"
		commonFalseVal = "0"
	}
	switch common.LogDatabaseType() {
	case common.DatabaseTypePostgreSQL:
		logGroupCol = `"group"`
		logKeyCol = `"key"`
	default:
		logGroupCol = "`group`"
		logKeyCol = "`key`"
	}
	// log sql type and database type
	//common.SysLog("Using Log SQL Type: " + common.LogSqlType)
}

var DB *gorm.DB

var LOG_DB *gorm.DB

func CheckSetup() {
	setup := GetSetup()
	if setup == nil {
		// No setup record exists, check if we have a root user
		if RootUserExists() {
			common.SysLog("system is not initialized, but root user exists")
			// Create setup record
			newSetup := Setup{
				Version:       common.Version,
				InitializedAt: time.Now().Unix(),
			}
			err := DB.Create(&newSetup).Error
			if err != nil {
				common.SysLog("failed to create setup record: " + err.Error())
			}
			constant.Setup = true
		} else {
			common.SysLog("system is not initialized and no root user exists")
			constant.Setup = false
		}
	} else {
		// Setup record exists, system is initialized
		common.SysLog("system is already initialized at: " + time.Unix(setup.InitializedAt, 0).String())
		constant.Setup = true
	}
}

func isClickHouseDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "clickhouse://") ||
		strings.HasPrefix(dsn, "tcp://") ||
		strings.HasPrefix(dsn, "http://") ||
		strings.HasPrefix(dsn, "https://")
}

func normalizeClickHouseDSN(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Scheme != "https" {
		return dsn
	}
	query := parsed.Query()
	if _, ok := query["secure"]; !ok {
		query.Set("secure", "true")
		parsed.RawQuery = query.Encode()
	}
	return parsed.String()
}

func chooseDB(envName string, isLog bool) (*gorm.DB, common.DatabaseType, error) {
	dsn := os.Getenv(envName)
	if dsn != "" {
		if isClickHouseDSN(dsn) {
			if !isLog {
				return nil, "", fmt.Errorf("%s does not support ClickHouse; use SQLite, MySQL, or PostgreSQL for the primary database and LOG_SQL_DSN for ClickHouse logs", envName)
			}
			common.SysLog("using ClickHouse as log database")
			db, err := gorm.Open(clickhouse.Open(normalizeClickHouseDSN(dsn)), &gorm.Config{
				PrepareStmt: false,
			})
			return db, common.DatabaseTypeClickHouse, err
		}
		if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
			// Use PostgreSQL
			common.SysLog("using PostgreSQL as database")
			db, err := gorm.Open(postgres.New(postgres.Config{
				DSN:                  dsn,
				PreferSimpleProtocol: true, // disables implicit prepared statement usage
			}), &gorm.Config{
				PrepareStmt: true, // precompile SQL
			})
			return db, common.DatabaseTypePostgreSQL, err
		}
		if strings.HasPrefix(dsn, "local") {
			common.SysLog("SQL_DSN not set, using SQLite as database")
			db, err := gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{
				PrepareStmt: true, // precompile SQL
			})
			return db, common.DatabaseTypeSQLite, err
		}
		// Use MySQL
		common.SysLog("using MySQL as database")
		// check parseTime
		if !strings.Contains(dsn, "parseTime") {
			if strings.Contains(dsn, "?") {
				dsn += "&parseTime=true"
			} else {
				dsn += "?parseTime=true"
			}
		}
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
			PrepareStmt: true, // precompile SQL
		})
		return db, common.DatabaseTypeMySQL, err
	}
	// Use SQLite
	common.SysLog("SQL_DSN not set, using SQLite as database")
	db, err := gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{
		PrepareStmt: true, // precompile SQL
	})
	return db, common.DatabaseTypeSQLite, err
}

func InitDB() error {
	return withDatabaseStartupRetry("main database", initDBOnce)
}

func initDBOnce() (err error) {
	db, dbType, err := chooseDB("SQL_DSN", false)
	if err == nil {
		common.SetMainDatabaseType(dbType)
		if os.Getenv("LOG_SQL_DSN") == "" {
			common.SetLogDatabaseType(dbType)
		}
		initCol()
		if common.DebugEnabled {
			db = db.Debug()
		}
		DB = db
		defer func() {
			if err != nil {
				_ = closeDB(DB)
			}
		}()
		// MySQL charset/collation startup check: ensure Chinese-capable charset
		if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
			if err := checkMySQLChineseSupport(DB); err != nil {
				panic(err)
			}
		}
		sqlDB, err := DB.DB()
		if err != nil {
			return err
		}
		configureSQLPool(sqlDB)

		if !common.IsMasterNode {
			return nil
		}
		if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
			//_, _ = sqlDB.Exec("ALTER TABLE channels MODIFY model_mapping TEXT;") // TODO: delete this line when most users have upgraded
		}
		common.SysLog("database migration started")
		err = migrateDB()
		return err
	}
	return err
}

func InitLogDB() (err error) {
	if os.Getenv("LOG_SQL_DSN") == "" {
		LOG_DB = DB
		common.SetLogDatabaseType(common.MainDatabaseType())
		initCol()
		return migrateLOGDB()
	}
	return withDatabaseStartupRetry("log database", initLogDBOnce)
}

func initLogDBOnce() (err error) {
	db, dbType, err := chooseDB("LOG_SQL_DSN", true)
	if err == nil {
		common.SetLogDatabaseType(dbType)
		initCol()
		if common.DebugEnabled {
			db = db.Debug()
		}
		LOG_DB = db
		defer func() {
			if err != nil {
				_ = closeDB(LOG_DB)
			}
		}()
		// If log DB is MySQL, also ensure Chinese-capable charset
		if common.UsingLogDatabase(common.DatabaseTypeMySQL) {
			if err := checkMySQLChineseSupport(LOG_DB); err != nil {
				panic(err)
			}
		}
		sqlDB, err := LOG_DB.DB()
		if err != nil {
			return err
		}
		configureSQLPool(sqlDB)

		if !common.IsMasterNode {
			return nil
		}
		common.SysLog("database migration started")
		err = migrateLOGDB()
		return err
	}
	return err
}

func withDatabaseStartupRetry(label string, fn func() error) error {
	timeoutSeconds := common.GetEnvOrDefault("DB_STARTUP_CONNECT_TIMEOUT_SECONDS", defaultDBStartupConnectTimeoutSeconds)
	intervalMs := common.GetEnvOrDefault("DB_STARTUP_CONNECT_RETRY_INTERVAL_MS", defaultDBStartupConnectRetryIntervalMs)
	if timeoutSeconds <= 0 || intervalMs <= 0 {
		return fn()
	}

	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	attempt := 0
	for {
		attempt++
		err := fn()
		if err == nil {
			return nil
		}
		if time.Now().Add(time.Duration(intervalMs) * time.Millisecond).After(deadline) {
			return err
		}
		// Startup only: Docker Compose and managed databases can report healthy before
		// PostgreSQL/MySQL accepts every migration query. Keep retry finite and visible.
		common.SysLog(fmt.Sprintf("%s initialization failed on attempt %d, retrying in %dms: %v", label, attempt, intervalMs, err))
		time.Sleep(time.Duration(intervalMs) * time.Millisecond)
	}
}

type sqlPool interface {
	SetMaxIdleConns(n int)
	SetMaxOpenConns(n int)
	SetConnMaxLifetime(d time.Duration)
}

func configureSQLPool(sqlDB sqlPool) {
	sqlDB.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", defaultSQLMaxIdleConns))
	sqlDB.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", defaultSQLMaxOpenConns))
	sqlDB.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", defaultSQLMaxLifetime)))
}

func migrateDB() error {
	// Migrate price_amount column from float/double to decimal for existing tables
	migrateSubscriptionPlanPriceAmount()
	// Migrate model_limits column from varchar to text for existing tables
	if err := migrateTokenModelLimitsToText(); err != nil {
		return err
	}
	if err := migrateReferralCleanup(); err != nil {
		return err
	}
	if err := migrateRiskCleanup(); err != nil {
		return err
	}

	err := DB.AutoMigrate(
		&Channel{},
		&Token{},
		&User{},
		&ReferralAffiliate{},
		&ReferralBinding{},
		&ReferralClick{},
		&ReferralCommissionAccount{},
		&ReferralCommission{},
		&ReferralCommissionLedger{},
		&ReferralAsset{},
		&ReferralWithdrawal{},
		&ReferralWithdrawalItem{},
		&ReferralSettlementBatch{},
		&ReferralCommissionJob{},
		&ReferralAdminAuditLog{},
		&PasskeyCredential{},
		&AuthzRole{},
		&CasbinRule{},
		&Option{},
		&Redemption{},
		&Ability{},
		&Log{},
		&Midjourney{},
		&TopUp{},
		&QuotaData{},
		&Task{},
		&TaskDispatchState{},
		&ImageTaskChannelLease{},
		&ImageTaskClientTaskIDLock{},
		&TaskSettlementRecord{},
		&Model{},
		&Vendor{},
		&PrefillGroup{},
		&Setup{},
		&TwoFA{},
		&TwoFABackupCode{},
		&Checkin{},
		&UserLoginIdentifier{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&SubscriptionPreConsumeRecord{},
		&CustomOAuthProvider{},
		&UserOAuthBinding{},
		&PerfMetric{},
		&TokenUsageReset{},
		&TokenUsageDaily{},
		&Ticket{},
		&TicketMessage{},
		&TicketAttachment{},
		&TicketSequence{},
		&TelegramPushRecord{},
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
	)
	if err != nil {
		return err
	}
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		if err := ensureSubscriptionPlanTableSQLite(); err != nil {
			return err
		}
	} else {
		if err := DB.AutoMigrate(&SubscriptionPlan{}); err != nil {
			return err
		}
	}
	if err := ensureUserEmailCanonicalUniqueIndex(); err != nil {
		return err
	}
	if err := ensureUserLoginIdentifiers(); err != nil {
		return err
	}
	if err := ensureRechargeOrderIndexes(); err != nil {
		return err
	}
	if err := migrateImageTaskPortableStorageNodes(); err != nil {
		return err
	}
	cleanupConversationArtifactOptions()
	return nil
}

func migrateReferralCleanup() error {
	// These legacy option keys and column names are intentionally kept here
	// only as a schema cleanup list. They are not used by current business
	// logic and exist solely so fresh deployments can drop the abandoned
	// invite-system schema deterministically.
	legacyOptionKeys := []string{
		"QuotaForInviter",
		"QuotaForInvitee",
		"InviteCode",
		"InviteUserQuota",
		"InviterBonus",
		"InviteeBonus",
	}
	if DB.Migrator().HasTable(&Option{}) {
		if err := DB.Where("key IN ?", legacyOptionKeys).Delete(&Option{}).Error; err != nil {
			return err
		}
	}

	return nil
}

func migrateRiskCleanup() error {
	if !common.GetEnvOrDefaultBool(dropLegacyRiskTablesEnv, false) {
		common.SysLog("legacy risk table cleanup skipped; set " + dropLegacyRiskTablesEnv + "=true to drop legacy risk tables")
		return nil
	}

	legacyRiskTables := []string{
		"risk_whitelists",
		"risk_actions",
		"risk_events",
	}
	for _, tableName := range legacyRiskTables {
		if !DB.Migrator().HasTable(tableName) {
			continue
		}
		if err := DB.Migrator().DropTable(tableName); err != nil {
			return fmt.Errorf("drop legacy risk table %s: %w", tableName, err)
		}
		common.SysLog("dropped legacy risk table: " + tableName)
	}
	return nil
}

func migrateDBFast() error {
	if err := migrateRiskCleanup(); err != nil {
		return err
	}

	var wg sync.WaitGroup

	migrations := []struct {
		model interface{}
		name  string
	}{
		{&Channel{}, "Channel"},
		{&Token{}, "Token"},
		{&User{}, "User"},
		{&ReferralAffiliate{}, "ReferralAffiliate"},
		{&ReferralBinding{}, "ReferralBinding"},
		{&ReferralClick{}, "ReferralClick"},
		{&ReferralCommissionAccount{}, "ReferralCommissionAccount"},
		{&ReferralCommission{}, "ReferralCommission"},
		{&ReferralCommissionLedger{}, "ReferralCommissionLedger"},
		{&ReferralAsset{}, "ReferralAsset"},
		{&ReferralWithdrawal{}, "ReferralWithdrawal"},
		{&ReferralWithdrawalItem{}, "ReferralWithdrawalItem"},
		{&ReferralSettlementBatch{}, "ReferralSettlementBatch"},
		{&ReferralCommissionJob{}, "ReferralCommissionJob"},
		{&ReferralAdminAuditLog{}, "ReferralAdminAuditLog"},
		{&PasskeyCredential{}, "PasskeyCredential"},
		{&AuthzRole{}, "AuthzRole"},
		{&CasbinRule{}, "CasbinRule"},
		{&Option{}, "Option"},
		{&Redemption{}, "Redemption"},
		{&Ability{}, "Ability"},
		{&Log{}, "Log"},
		{&Midjourney{}, "Midjourney"},
		{&TopUp{}, "TopUp"},
		{&QuotaData{}, "QuotaData"},
		{&Task{}, "Task"},
		{&TaskDispatchState{}, "TaskDispatchState"},
		{&ImageTaskChannelLease{}, "ImageTaskChannelLease"},
		{&ImageTaskClientTaskIDLock{}, "ImageTaskClientTaskIDLock"},
		{&TaskSettlementRecord{}, "TaskSettlementRecord"},
		{&Model{}, "Model"},
		{&Vendor{}, "Vendor"},
		{&PrefillGroup{}, "PrefillGroup"},
		{&Setup{}, "Setup"},
		{&TwoFA{}, "TwoFA"},
		{&TwoFABackupCode{}, "TwoFABackupCode"},
		{&Checkin{}, "Checkin"},
		{&UserLoginIdentifier{}, "UserLoginIdentifier"},
		{&SubscriptionOrder{}, "SubscriptionOrder"},
		{&UserSubscription{}, "UserSubscription"},
		{&SubscriptionPreConsumeRecord{}, "SubscriptionPreConsumeRecord"},
		{&CustomOAuthProvider{}, "CustomOAuthProvider"},
		{&UserOAuthBinding{}, "UserOAuthBinding"},
		{&PerfMetric{}, "PerfMetric"},
		{&TokenUsageReset{}, "TokenUsageReset"},
		{&TokenUsageDaily{}, "TokenUsageDaily"},
		{&Ticket{}, "Ticket"},
		{&TicketMessage{}, "TicketMessage"},
		{&TicketAttachment{}, "TicketAttachment"},
		{&TicketSequence{}, "TicketSequence"},
		{&TelegramPushRecord{}, "TelegramPushRecord"},
		{&SystemInstance{}, "SystemInstance"},
		{&SystemTask{}, "SystemTask"},
		{&SystemTaskLock{}, "SystemTaskLock"},
	}
	// 动态计算migration数量，确保errChan缓冲区足够大
	errChan := make(chan error, len(migrations))

	for _, m := range migrations {
		wg.Add(1)
		go func(model interface{}, name string) {
			defer wg.Done()
			if err := DB.AutoMigrate(model); err != nil {
				errChan <- fmt.Errorf("failed to migrate %s: %v", name, err)
			}
		}(m.model, m.name)
	}

	// Wait for all migrations to complete
	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		if err := ensureSubscriptionPlanTableSQLite(); err != nil {
			return err
		}
	} else {
		if err := DB.AutoMigrate(&SubscriptionPlan{}); err != nil {
			return err
		}
	}
	if err := ensureUserEmailCanonicalUniqueIndex(); err != nil {
		return err
	}
	if err := ensureUserLoginIdentifiers(); err != nil {
		return err
	}
	if err := ensureRechargeOrderIndexes(); err != nil {
		return err
	}
	if err := migrateImageTaskPortableStorageNodes(); err != nil {
		return err
	}
	cleanupConversationArtifactOptions()
	common.SysLog("database migrated")
	return nil
}

func migrateImageTaskPortableStorageNodes() error {
	const batchSize = 1000
	var total int64
	for {
		var ids []int64
		privateDataWhere, privateDataArgs := imageTaskPortablePrivateDataWhere()
		err := DB.Model(&Task{}).
			Where("platform = ?", constant.TaskPlatformImage).
			Where("(storage_node = '' OR storage_node IS NULL)").
			Where(privateDataWhere, privateDataArgs...).
			Order("id ASC").
			Limit(batchSize).
			Pluck("id", &ids).Error
		if err != nil || len(ids) == 0 {
			if total > 0 {
				common.SysLog(fmt.Sprintf("migrated %d portable image task storage nodes", total))
			}
			return err
		}
		result := DB.Model(&Task{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"storage_node": ImageTaskPortableStorageNode,
				"updated_at":   time.Now().Unix(),
			})
		if result.Error != nil {
			return result.Error
		}
		total += result.RowsAffected
		if len(ids) < batchSize {
			if total > 0 {
				common.SysLog(fmt.Sprintf("migrated %d portable image task storage nodes", total))
			}
			return nil
		}
	}
}

func imageTaskPortablePrivateDataWhere() (string, []any) {
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		return "(private_data ->> 'request_body_portable') = ? AND COALESCE(private_data ->> 'request_body_base64', '') <> ''", []any{"true"}
	}
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return "JSON_UNQUOTE(JSON_EXTRACT(private_data, '$.request_body_portable')) = ? AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(private_data, '$.request_body_base64')), '') <> ''", []any{"true"}
	}
	column := "CAST(private_data AS TEXT)"
	return "(" + column + " LIKE ? OR " + column + " LIKE ?) AND (" + column + " LIKE ? OR " + column + " LIKE ?)", []any{
		`%"request_body_portable":true%`,
		`%"request_body_portable": true%`,
		`%"request_body_base64":"%`,
		`%"request_body_base64": "%`,
	}
}

func migrateLOGDB() error {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return migrateClickHouseLogDB()
	}
	if err := LOG_DB.AutoMigrate(&Log{}); err != nil {
		return err
	}
	cleanupConversationArtifacts()
	return nil
}

func migrateClickHouseLogDB() error {
	ttlDays := clickHouseLogTTLDays()
	if err := LOG_DB.Exec(clickHouseLogCreateTableSQL(ttlDays)).Error; err != nil {
		return err
	}
	return syncClickHouseLogTTL(ttlDays)
}

func clickHouseLogTTLDays() int {
	ttlDays := common.GetEnvOrDefault("LOG_SQL_CLICKHOUSE_TTL_DAYS", 0)
	if ttlDays < 0 {
		return 0
	}
	return ttlDays
}

func clickHouseLogTTLExpression(ttlDays int) string {
	if ttlDays <= 0 {
		return ""
	}
	return fmt.Sprintf("toDateTime(created_at) + INTERVAL %d DAY DELETE", ttlDays)
}

func clickHouseLogTTLClause(ttlDays int) string {
	expression := clickHouseLogTTLExpression(ttlDays)
	if expression == "" {
		return ""
	}
	return "\nTTL " + expression
}

func clickHouseLogCreateTableSQL(ttlDays int) string {
	return fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS logs (
	id Int64 DEFAULT 0,
	user_id Int32 DEFAULT 0,
	created_at Int64 DEFAULT 0,
	type Int32 DEFAULT 0,
	content String DEFAULT '',
	username String DEFAULT '',
	token_name String DEFAULT '',
	model_name String DEFAULT '',
	quota Int32 DEFAULT 0,
	prompt_tokens Int32 DEFAULT 0,
	completion_tokens Int32 DEFAULT 0,
	use_time Int32 DEFAULT 0,
	is_stream UInt8 DEFAULT 0,
	channel_id Int32 DEFAULT 0,
	token_id Int32 DEFAULT 0,
	`+"`group`"+` String DEFAULT '',
	ip String DEFAULT '',
	request_id String DEFAULT '',
	upstream_request_id String DEFAULT '',
	other String DEFAULT ''
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(toDateTime(created_at))
ORDER BY (created_at, request_id)%s`, clickHouseLogTTLClause(ttlDays))
}

func syncClickHouseLogTTL(ttlDays int) error {
	expression := clickHouseLogTTLExpression(ttlDays)
	if expression != "" {
		return LOG_DB.Exec("ALTER TABLE logs MODIFY TTL " + expression).Error
	}

	hasTTL, err := clickHouseLogTableHasTTL()
	if err != nil {
		return err
	}
	if !hasTTL {
		return nil
	}
	return LOG_DB.Exec("ALTER TABLE logs REMOVE TTL").Error
}

func clickHouseLogTableHasTTL() (bool, error) {
	var createTableSQL string
	if err := LOG_DB.Raw("SHOW CREATE TABLE logs").Scan(&createTableSQL).Error; err != nil {
		return false, err
	}
	return clickHouseCreateTableHasTTL(createTableSQL), nil
}

func clickHouseCreateTableHasTTL(createTableSQL string) bool {
	upperSQL := strings.ToUpper(createTableSQL)
	return strings.Contains(upperSQL, "\nTTL ") || strings.Contains(upperSQL, " TTL ")
}

func cleanupConversationArtifacts() {
	if LOG_DB == nil {
		return
	}
	for _, tableName := range legacyConversationArtifactTables {
		if err := LOG_DB.Migrator().DropTable(tableName); err != nil {
			common.SysLog(fmt.Sprintf("failed to drop legacy conversation table %s: %v", tableName, err))
		}
	}
	if err := os.RemoveAll("data/conversation-exports"); err != nil {
		common.SysLog("failed to remove legacy conversation export files: " + err.Error())
	}
}

func cleanupConversationArtifactOptions() {
	if DB == nil {
		return
	}
	if err := DB.Delete(&Option{}, legacyConversationArtifactOptions).Error; err != nil {
		common.SysLog("failed to remove legacy conversation options: " + err.Error())
	}
}

func ensureUserEmailCanonicalUniqueIndex() error {
	if !DB.Migrator().HasColumn(&User{}, "email_canonical") {
		if err := DB.Migrator().AddColumn(&User{}, "EmailCanonical"); err != nil {
			return err
		}
	}
	if err := DB.Model(&User{}).
		Where("email IS NULL OR TRIM(email) = ''").
		Update("email_canonical", nil).Error; err != nil {
		return err
	}
	if err := DB.Model(&User{}).
		Where("email IS NOT NULL AND TRIM(email) <> ''").
		Update("email_canonical", gorm.Expr("LOWER(TRIM(email))")).Error; err != nil {
		return err
	}

	type duplicateEmail struct {
		EmailCanonical string
		Count          int64
	}
	var duplicate duplicateEmail
	if err := DB.Model(&User{}).
		Select("email_canonical, COUNT(*) AS count").
		Where("email_canonical IS NOT NULL AND email_canonical <> ''").
		Group("email_canonical").
		Having("COUNT(*) > 1").
		Limit(1).
		Scan(&duplicate).Error; err != nil {
		return err
	}
	if duplicate.Count > 1 {
		return fmt.Errorf("duplicate user email exists before creating unique index: %s", duplicate.EmailCanonical)
	}

	const indexName = "idx_users_email_canonical_unique"
	if DB.Migrator().HasIndex(&User{}, indexName) {
		return nil
	}
	return DB.Exec("CREATE UNIQUE INDEX " + indexName + " ON users (email_canonical)").Error
}

func ensureUserLoginIdentifiers() error {
	if err := DB.AutoMigrate(&UserLoginIdentifier{}); err != nil {
		return err
	}
	if err := rejectExistingUserLoginIdentifierConflicts(); err != nil {
		return err
	}
	var users []User
	if err := DB.Unscoped().Find(&users).Error; err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&UserLoginIdentifier{}).Error; err != nil {
			return err
		}
		for _, user := range users {
			if err := syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, user.Email); err != nil {
				return err
			}
		}
		return nil
	})
}

func rejectExistingUserLoginIdentifierConflicts() error {
	var users []User
	if err := DB.Unscoped().Select("id", "username", "email_canonical").Find(&users).Error; err != nil {
		return err
	}
	ownerByIdentifier := map[string]int{}
	for _, user := range users {
		email := ""
		if user.EmailCanonical != nil {
			email = *user.EmailCanonical
		}
		if hasDuplicateUserLoginIdentifiers(user.Username, email) {
			return fmt.Errorf("duplicate user login identifier exists on user %d: %s", user.Id, strings.TrimSpace(user.Username))
		}
		for identifier := range getUserLoginIdentifiers(user.Username, email) {
			if ownerId, ok := ownerByIdentifier[identifier]; ok && ownerId != user.Id {
				return fmt.Errorf("duplicate user login identifier exists before syncing login identifiers: %s", identifier)
			}
			ownerByIdentifier[identifier] = user.Id
		}
	}
	return nil
}

type sqliteColumnDef struct {
	Name string
	DDL  string
}

func ensureSubscriptionPlanTableSQLite() error {
	if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return nil
	}
	tableName := "subscription_plans"
	if !DB.Migrator().HasTable(tableName) {
		createSQL := `CREATE TABLE ` + "`" + tableName + "`" + ` (
` + "`id`" + ` integer,
` + "`title`" + ` varchar(128) NOT NULL,
` + "`subtitle`" + ` varchar(255) DEFAULT '',
` + "`price_amount`" + ` decimal(10,6) NOT NULL,
` + "`currency`" + ` varchar(8) NOT NULL DEFAULT 'USD',
` + "`duration_unit`" + ` varchar(16) NOT NULL DEFAULT 'month',
` + "`duration_value`" + ` integer NOT NULL DEFAULT 1,
` + "`custom_seconds`" + ` bigint NOT NULL DEFAULT 0,
` + "`enabled`" + ` numeric DEFAULT 1,
` + "`sort_order`" + ` integer DEFAULT 0,
` + "`allow_balance_pay`" + ` numeric DEFAULT 1,
` + "`allow_wallet_overflow`" + ` numeric DEFAULT 1,
` + "`stripe_price_id`" + ` varchar(128) DEFAULT '',
` + "`creem_product_id`" + ` varchar(128) DEFAULT '',
` + "`waffo_pancake_product_id`" + ` varchar(128) DEFAULT '',
` + "`max_purchase_per_user`" + ` integer DEFAULT 0,
` + "`upgrade_group`" + ` varchar(64) DEFAULT '',
` + "`grant_groups`" + ` text DEFAULT '',
` + "`downgrade_group`" + ` varchar(64) DEFAULT '',
` + "`total_amount`" + ` bigint NOT NULL DEFAULT 0,
` + "`quota_reset_period`" + ` varchar(16) DEFAULT 'never',
` + "`quota_reset_custom_seconds`" + ` bigint DEFAULT 0,
` + "`created_at`" + ` bigint,
` + "`updated_at`" + ` bigint,
PRIMARY KEY (` + "`id`" + `)
)`
		return DB.Exec(createSQL).Error
	}
	var cols []struct {
		Name string `gorm:"column:name"`
	}
	if err := DB.Raw("PRAGMA table_info(`" + tableName + "`)").Scan(&cols).Error; err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		existing[c.Name] = struct{}{}
	}
	required := []sqliteColumnDef{
		{Name: "title", DDL: "`title` varchar(128) NOT NULL"},
		{Name: "subtitle", DDL: "`subtitle` varchar(255) DEFAULT ''"},
		{Name: "price_amount", DDL: "`price_amount` decimal(10,6) NOT NULL"},
		{Name: "currency", DDL: "`currency` varchar(8) NOT NULL DEFAULT 'USD'"},
		{Name: "duration_unit", DDL: "`duration_unit` varchar(16) NOT NULL DEFAULT 'month'"},
		{Name: "duration_value", DDL: "`duration_value` integer NOT NULL DEFAULT 1"},
		{Name: "custom_seconds", DDL: "`custom_seconds` bigint NOT NULL DEFAULT 0"},
		{Name: "enabled", DDL: "`enabled` numeric DEFAULT 1"},
		{Name: "sort_order", DDL: "`sort_order` integer DEFAULT 0"},
		{Name: "allow_balance_pay", DDL: "`allow_balance_pay` numeric DEFAULT 1"},
		{Name: "allow_wallet_overflow", DDL: "`allow_wallet_overflow` numeric DEFAULT 1"},
		{Name: "stripe_price_id", DDL: "`stripe_price_id` varchar(128) DEFAULT ''"},
		{Name: "creem_product_id", DDL: "`creem_product_id` varchar(128) DEFAULT ''"},
		{Name: "waffo_pancake_product_id", DDL: "`waffo_pancake_product_id` varchar(128) DEFAULT ''"},
		{Name: "max_purchase_per_user", DDL: "`max_purchase_per_user` integer DEFAULT 0"},
		{Name: "upgrade_group", DDL: "`upgrade_group` varchar(64) DEFAULT ''"},
		{Name: "grant_groups", DDL: "`grant_groups` text DEFAULT ''"},
		{Name: "downgrade_group", DDL: "`downgrade_group` varchar(64) DEFAULT ''"},
		{Name: "total_amount", DDL: "`total_amount` bigint NOT NULL DEFAULT 0"},
		{Name: "quota_reset_period", DDL: "`quota_reset_period` varchar(16) DEFAULT 'never'"},
		{Name: "quota_reset_custom_seconds", DDL: "`quota_reset_custom_seconds` bigint DEFAULT 0"},
		{Name: "created_at", DDL: "`created_at` bigint"},
		{Name: "updated_at", DDL: "`updated_at` bigint"},
	}
	for _, col := range required {
		if _, ok := existing[col.Name]; ok {
			continue
		}
		if err := DB.Exec("ALTER TABLE `" + tableName + "` ADD COLUMN " + col.DDL).Error; err != nil {
			return err
		}
	}
	return nil
}

// migrateTokenModelLimitsToText migrates model_limits column from varchar(1024) to text
// This is safe to run multiple times - it checks the column type first
func migrateTokenModelLimitsToText() error {
	// SQLite uses type affinity, so TEXT and VARCHAR are effectively the same — no migration needed
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return nil
	}

	tableName := "tokens"
	columnName := "model_limits"

	if !DB.Migrator().HasTable(tableName) {
		return nil
	}

	if !DB.Migrator().HasColumn(&Token{}, columnName) {
		return nil
	}

	var alterSQL string
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		var dataType string
		if err := DB.Raw(`SELECT data_type FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&dataType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if dataType == "text" {
			return nil
		}
		alterSQL = fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s TYPE text`, tableName, columnName)
	} else if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		var columnType string
		if err := DB.Raw(`SELECT COLUMN_TYPE FROM information_schema.columns
				WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&columnType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if strings.ToLower(columnType) == "text" {
			return nil
		}
		alterSQL = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s text", tableName, columnName)
	} else {
		return nil
	}

	if alterSQL != "" {
		if err := DB.Exec(alterSQL).Error; err != nil {
			return fmt.Errorf("failed to migrate %s.%s to text: %w", tableName, columnName, err)
		}
		common.SysLog(fmt.Sprintf("Successfully migrated %s.%s to text", tableName, columnName))
	}
	return nil
}

// migrateSubscriptionPlanPriceAmount migrates price_amount column from float/double to decimal(10,6)
// This is safe to run multiple times - it checks the column type first
func migrateSubscriptionPlanPriceAmount() {
	// SQLite doesn't support ALTER COLUMN, and its type affinity handles this automatically
	// Skip early to avoid GORM parsing the existing table DDL which may cause issues
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return
	}

	tableName := "subscription_plans"
	columnName := "price_amount"

	// Check if table exists first
	if !DB.Migrator().HasTable(tableName) {
		return
	}

	// Check if column exists
	if !DB.Migrator().HasColumn(&SubscriptionPlan{}, columnName) {
		return
	}

	var alterSQL string
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		// PostgreSQL: Check if already decimal/numeric
		var dataType string
		if err := DB.Raw(`SELECT data_type FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&dataType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if dataType == "numeric" {
			return // Already decimal/numeric
		}
		alterSQL = fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s TYPE decimal(10,6) USING %s::decimal(10,6)`,
			tableName, columnName, columnName)
	} else if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		// MySQL: Check if already decimal
		var columnType string
		if err := DB.Raw(`SELECT COLUMN_TYPE FROM information_schema.columns
				WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&columnType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if strings.HasPrefix(strings.ToLower(columnType), "decimal") {
			return // Already decimal
		}
		alterSQL = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s decimal(10,6) NOT NULL DEFAULT 0",
			tableName, columnName)
	} else {
		return
	}

	if alterSQL != "" {
		if err := DB.Exec(alterSQL).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to migrate %s.%s to decimal: %v", tableName, columnName, err))
		} else {
			common.SysLog(fmt.Sprintf("Successfully migrated %s.%s to decimal(10,6)", tableName, columnName))
		}
	}
}

func closeDB(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	err = sqlDB.Close()
	return err
}

func CloseDB() error {
	if LOG_DB != DB {
		err := closeDB(LOG_DB)
		if err != nil {
			return err
		}
	}
	return closeDB(DB)
}

// checkMySQLChineseSupport ensures the MySQL connection and current schema
// default charset/collation can store Chinese characters. It allows common
// Chinese-capable charsets (utf8mb4, utf8, gbk, big5, gb18030) and panics otherwise.
func checkMySQLChineseSupport(db *gorm.DB) error {
	// 仅检测：当前库默认字符集/排序规则 + 各表的排序规则（隐含字符集）

	// Read current schema defaults
	var schemaCharset, schemaCollation string
	err := db.Raw("SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = DATABASE()").Row().Scan(&schemaCharset, &schemaCollation)
	if err != nil {
		return fmt.Errorf("读取当前库默认字符集/排序规则失败 / Failed to read schema default charset/collation: %v", err)
	}

	toLower := func(s string) string { return strings.ToLower(s) }
	// Allowed charsets that can store Chinese text
	allowedCharsets := map[string]string{
		"utf8mb4": "utf8mb4_",
		"utf8":    "utf8_",
		"gbk":     "gbk_",
		"big5":    "big5_",
		"gb18030": "gb18030_",
	}
	isChineseCapable := func(cs, cl string) bool {
		csLower := toLower(cs)
		clLower := toLower(cl)
		if prefix, ok := allowedCharsets[csLower]; ok {
			if clLower == "" {
				return true
			}
			return strings.HasPrefix(clLower, prefix)
		}
		// 如果仅提供了排序规则，尝试按排序规则前缀判断
		for _, prefix := range allowedCharsets {
			if strings.HasPrefix(clLower, prefix) {
				return true
			}
		}
		return false
	}

	// 1) 当前库默认值必须支持中文
	if !isChineseCapable(schemaCharset, schemaCollation) {
		return fmt.Errorf("当前库默认字符集/排序规则不支持中文：schema(%s/%s)。请将库设置为 utf8mb4/utf8/gbk/big5/gb18030 / Schema default charset/collation is not Chinese-capable: schema(%s/%s). Please set to utf8mb4/utf8/gbk/big5/gb18030",
			schemaCharset, schemaCollation, schemaCharset, schemaCollation)
	}

	// 2) 所有物理表的排序规则（隐含字符集）必须支持中文
	type tableInfo struct {
		Name      string
		Collation *string
	}
	var tables []tableInfo
	if err := db.Raw("SELECT TABLE_NAME, TABLE_COLLATION FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'").Scan(&tables).Error; err != nil {
		return fmt.Errorf("读取表排序规则失败 / Failed to read table collations: %v", err)
	}

	var badTables []string
	for _, t := range tables {
		// NULL 或空表示继承库默认设置，已在上面校验库默认，视为通过
		if t.Collation == nil || *t.Collation == "" {
			continue
		}
		cl := *t.Collation
		// 仅凭排序规则判断是否中文可用
		ok := false
		lower := strings.ToLower(cl)
		for _, prefix := range allowedCharsets {
			if strings.HasPrefix(lower, prefix) {
				ok = true
				break
			}
		}
		if !ok {
			badTables = append(badTables, fmt.Sprintf("%s(%s)", t.Name, cl))
		}
	}

	if len(badTables) > 0 {
		// 限制输出数量以避免日志过长
		maxShow := 20
		shown := badTables
		if len(shown) > maxShow {
			shown = shown[:maxShow]
		}
		return fmt.Errorf(
			"存在不支持中文的表，请修复其排序规则/字符集。示例（最多展示 %d 项）：%v / Found tables not Chinese-capable. Please fix their collation/charset. Examples (showing up to %d): %v",
			maxShow, shown, maxShow, shown,
		)
	}
	return nil
}

var (
	lastPingTime time.Time
	pingMutex    sync.Mutex
)

func PingDB() error {
	pingMutex.Lock()
	defer pingMutex.Unlock()

	if time.Since(lastPingTime) < time.Second*10 {
		return nil
	}

	sqlDB, err := DB.DB()
	if err != nil {
		log.Printf("Error getting sql.DB from GORM: %v", err)
		return err
	}

	err = sqlDB.Ping()
	if err != nil {
		log.Printf("Error pinging DB: %v", err)
		return err
	}

	lastPingTime = time.Now()
	common.SysLog("Database pinged successfully")
	return nil
}
