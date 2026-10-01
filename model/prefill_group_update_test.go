package model

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func usePrefillPostgreSQL(t *testing.T) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		dsn = "host=/var/run/postgresql user=biubiubiu dbname=newapi_ci_review sslmode=disable"
	}
	schemaName := fmt.Sprintf("prefill_%d", time.Now().UnixNano())
	admin, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	require.NoError(t, err)
	adminSQL, err := admin.DB()
	require.NoError(t, err)
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schemaName).Error)
	t.Cleanup(func() {
		_ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error
		_ = adminSQL.Close()
	})

	isolated, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn + " options=-csearch_path=" + schemaName,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	require.NoError(t, err)
	isolatedSQL, err := isolated.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = isolatedSQL.Close() })
	require.NoError(t, isolated.AutoMigrate(&PrefillGroup{}))

	var version string
	require.NoError(t, isolated.Raw("SELECT version()").Scan(&version).Error)
	t.Log(version)

	previous := DB
	DB = isolated
	t.Cleanup(func() { DB = previous })
}

func TestPrefillGroupUpdatePreservesCreatedTimeOnPostgreSQL(t *testing.T) {
	usePrefillPostgreSQL(t)
	group := &PrefillGroup{
		Name:        "chat",
		Type:        "model",
		Items:       JSONValue(`["gpt-4o"]`),
		Description: "keep",
	}
	require.NoError(t, group.Insert())
	require.NotZero(t, group.CreatedTime)
	createdAt := group.CreatedTime

	updated := &PrefillGroup{
		Id:          group.Id,
		Name:        "chat-edit",
		Type:        "tag",
		Items:       JSONValue(`["gpt-4.1"]`),
		Description: "changed",
	}
	require.NoError(t, updated.Update())
	require.Equal(t, createdAt, updated.CreatedTime)
	require.GreaterOrEqual(t, updated.UpdatedTime, createdAt)

	var stored PrefillGroup
	require.NoError(t, DB.First(&stored, group.Id).Error)
	require.Equal(t, createdAt, stored.CreatedTime)
	require.Equal(t, "chat-edit", stored.Name)
	require.Equal(t, "tag", stored.Type)
	require.Equal(t, "changed", stored.Description)
	require.JSONEq(t, `["gpt-4.1"]`, string(stored.Items))
	require.False(t, stored.DeletedAt.Valid)
}

func TestPrefillGroupUpdateMissingOrDeletedDoesNotInsertOnPostgreSQL(t *testing.T) {
	usePrefillPostgreSQL(t)
	group := &PrefillGroup{Name: "gone", Type: "model", Items: JSONValue(`[]`)}
	require.NoError(t, group.Insert())
	require.NoError(t, DeletePrefillGroupByID(group.Id))

	err := (&PrefillGroup{Id: group.Id, Name: "revived", Type: "model", Items: JSONValue(`[]`)}).Update()
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	var deleted PrefillGroup
	require.NoError(t, DB.Unscoped().First(&deleted, group.Id).Error)
	require.True(t, deleted.DeletedAt.Valid)
	require.Equal(t, "gone", deleted.Name)
	require.NotZero(t, deleted.CreatedTime)

	err = (&PrefillGroup{Id: group.Id + 1000, Name: "new", Type: "model", Items: JSONValue(`[]`)}).Update()
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	var count int64
	require.NoError(t, DB.Unscoped().Model(&PrefillGroup{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestPrefillGroupRejectsBlankAndOversizedFieldsOnPostgreSQL(t *testing.T) {
	usePrefillPostgreSQL(t)
	group := &PrefillGroup{Name: "kept", Type: "model", Description: "desc", Items: JSONValue(`["a"]`)}
	require.NoError(t, group.Insert())

	require.ErrorIs(t, (&PrefillGroup{Id: group.Id, Name: "   ", Type: "model", Items: JSONValue(`["a"]`)}).Update(), ErrPrefillGroupNameTypeEmpty)
	require.ErrorIs(t, (&PrefillGroup{Id: group.Id, Name: strings.Repeat("名", 65), Type: "model", Items: JSONValue(`["a"]`)}).Update(), ErrPrefillGroupNameTooLong)
	require.ErrorIs(t, (&PrefillGroup{Id: group.Id, Name: "kept", Type: "model", Description: strings.Repeat("述", 256), Items: JSONValue(`["a"]`)}).Update(), ErrPrefillGroupDescriptionTooLong)
	require.ErrorIs(t, (&PrefillGroup{Id: group.Id, Name: "kept", Type: strings.Repeat("t", 33), Items: JSONValue(`["a"]`)}).Update(), ErrPrefillGroupTypeTooLong)

	var stored PrefillGroup
	require.NoError(t, DB.First(&stored, group.Id).Error)
	require.Equal(t, "kept", stored.Name)
	require.Equal(t, "model", stored.Type)
	require.Equal(t, "desc", stored.Description)
	require.Equal(t, group.CreatedTime, stored.CreatedTime)

	require.ErrorIs(t, (&PrefillGroup{Name: " ", Type: "model"}).Insert(), ErrPrefillGroupNameTypeEmpty)
	require.ErrorIs(t, (&PrefillGroup{Name: strings.Repeat("名", 65), Type: "model"}).Insert(), ErrPrefillGroupNameTooLong)
}
