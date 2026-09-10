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
