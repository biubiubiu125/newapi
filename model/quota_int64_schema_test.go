package model

import (
	"reflect"
	"strings"
	"testing"
)

func TestWalletQuotaFieldsUseInt64AndBigint(t *testing.T) {
	fields := []string{"Quota", "UsedQuota", "AffQuota", "AffHistoryQuota"}
	userType := reflect.TypeOf(User{})
	for _, name := range fields {
		field, ok := userType.FieldByName(name)
		if !ok {
			t.Fatalf("User.%s is missing", name)
		}
		if field.Type.Kind() != reflect.Int64 {
			t.Errorf("User.%s has type %s, want int64", name, field.Type)
		}
		if !strings.Contains(strings.ToLower(field.Tag.Get("gorm")), "bigint") {
			t.Errorf("User.%s gorm tag %q does not declare bigint", name, field.Tag.Get("gorm"))
		}
	}

	tokenType := reflect.TypeOf(Token{})
	for _, name := range []string{"RemainQuota", "UsedQuota"} {
		field, ok := tokenType.FieldByName(name)
		if !ok {
			t.Fatalf("Token.%s is missing", name)
		}
		if field.Type.Kind() != reflect.Int64 {
			t.Errorf("Token.%s has type %s, want int64", name, field.Type)
		}
		if !strings.Contains(strings.ToLower(field.Tag.Get("gorm")), "bigint") {
			t.Errorf("Token.%s gorm tag %q does not declare bigint", name, field.Tag.Get("gorm"))
		}
	}
}

func TestTaskAndLogQuotaColumnsUseBigint(t *testing.T) {
	taskField, ok := reflect.TypeOf(Task{}).FieldByName("Quota")
	if !ok {
		t.Fatal("Task.Quota is missing")
	}
	if !strings.Contains(strings.ToLower(taskField.Tag.Get("gorm")), "bigint") {
		t.Errorf("Task.Quota gorm tag %q does not declare bigint", taskField.Tag.Get("gorm"))
	}

	logField, ok := reflect.TypeOf(Log{}).FieldByName("Quota")
	if !ok {
		t.Fatal("Log.Quota is missing")
	}
	if !strings.Contains(strings.ToLower(logField.Tag.Get("gorm")), "bigint") {
		t.Errorf("Log.Quota gorm tag %q does not declare bigint", logField.Tag.Get("gorm"))
	}
}

func TestWalletQuotaSchemaIncludesTaskAndLogQuota(t *testing.T) {
	foundTask := false
	foundLog := false
	for _, column := range walletQuotaSchemaColumns {
		if column.table == "tasks" && column.column == "quota" {
			foundTask = true
		}
		if column.table == "logs" && column.column == "quota" {
			foundLog = true
		}
	}
	if !foundTask {
		t.Fatal("walletQuotaSchemaColumns is missing tasks.quota")
	}
	if !foundLog {
		t.Fatal("walletQuotaSchemaColumns is missing logs.quota")
	}
}
