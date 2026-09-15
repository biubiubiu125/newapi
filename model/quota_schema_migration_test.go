package model

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type testQuotaSchemaColumn struct {
	name         string
	databaseType string
}

func (c testQuotaSchemaColumn) Name() string                      { return c.name }
func (c testQuotaSchemaColumn) DatabaseTypeName() string          { return c.databaseType }
func (c testQuotaSchemaColumn) ColumnType() (string, bool)        { return c.databaseType, true }
func (c testQuotaSchemaColumn) PrimaryKey() (bool, bool)          { return false, true }
func (c testQuotaSchemaColumn) AutoIncrement() (bool, bool)       { return false, true }
func (c testQuotaSchemaColumn) Length() (int64, bool)             { return 0, false }
func (c testQuotaSchemaColumn) DecimalSize() (int64, int64, bool) { return 0, 0, false }
func (c testQuotaSchemaColumn) Nullable() (bool, bool)            { return true, true }
func (c testQuotaSchemaColumn) Unique() (bool, bool)              { return false, true }
func (c testQuotaSchemaColumn) ScanType() reflect.Type            { return reflect.TypeOf(int64(0)) }
func (c testQuotaSchemaColumn) Comment() (string, bool)           { return "", false }
func (c testQuotaSchemaColumn) DefaultValue() (string, bool)      { return "", false }

func TestMigrateQuotaSchemaSQLiteIsIdempotentAndPreservesWideValues(t *testing.T) {
	setupRiskCleanupTestDB(t)

	require.NoError(t, MigrateQuotaSchema(DB))
	require.NoError(t, MigrateQuotaSchema(DB))
	require.NoError(t, ValidateQuotaSchema(DB))

	const quota int64 = int64(1<<32 + 123)
	user := &User{Id: 9401, Username: "quota-schema-user", Password: "password", Quota: quota}
	require.NoError(t, DB.Create(user).Error)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	require.Equal(t, quota, reloaded.Quota)
}

func TestQuotaDatabaseTypeRejectsNarrowOrUnsignedTypes(t *testing.T) {
	require.False(t, quotaDatabaseTypeIsWideEnough("INT", "postgres"))
	require.False(t, quotaDatabaseTypeIsWideEnough("BIGINT UNSIGNED", "mysql"))
	require.True(t, quotaDatabaseTypeIsWideEnough("BIGINT", "postgres"))
	require.True(t, quotaDatabaseTypeIsWideEnough("INT8", "postgres"))
	require.True(t, quotaDatabaseTypeIsWideEnough("INTEGER", "sqlite"))
}

func TestQuotaColumnNeedsWideningSkipsAlreadyWideTypes(t *testing.T) {
	require.False(t, quotaColumnNeedsWidening(testQuotaSchemaColumn{databaseType: "BIGINT"}, "postgres"))
	require.False(t, quotaColumnNeedsWidening(testQuotaSchemaColumn{databaseType: "INT8"}, "postgres"))
	require.False(t, quotaColumnNeedsWidening(testQuotaSchemaColumn{databaseType: "BIGINT"}, "mysql"))
	require.True(t, quotaColumnNeedsWidening(testQuotaSchemaColumn{databaseType: "INTEGER"}, "postgres"))
	require.True(t, quotaColumnNeedsWidening(testQuotaSchemaColumn{databaseType: "INT"}, "mysql"))
}

func TestMigrateQuotaSchemaRejectsUnsupportedDialects(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	// A nil handle is the portable way to exercise the validation boundary
	// without opening a production database.
	require.EqualError(t, ValidateQuotaSchema(nil), "validate quota schema: database is nil")
	require.EqualError(t, MigrateQuotaSchema(nil), "migrate quota schema: database is nil")
}

func TestMigrateQuotaSchemaPostgresIncludesTaskAndLogQuota(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_DSN to run postgres quota schema test")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.AutoMigrate(&Task{}, &Log{}))
	require.NoError(t, MigrateQuotaSchema(db))
	require.NoError(t, ValidateQuotaSchema(db))
}
