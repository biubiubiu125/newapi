package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

func ensureRechargeOrderIndexes() error {
	indexes := []struct {
		model        interface{}
		table        string
		name         string
		columns      string
		mysqlColumns string
	}{
		{model: &TopUp{}, table: "top_ups", name: "idx_top_ups_user_create", columns: "(user_id, create_time, id)"},
		{model: &TopUp{}, table: "top_ups", name: "idx_top_ups_status_create", columns: "(status, create_time, id)", mysqlColumns: "(status(32), create_time, id)"},
		{model: &TopUp{}, table: "top_ups", name: "idx_top_ups_status_complete", columns: "(status, complete_time, id)", mysqlColumns: "(status(32), complete_time, id)"},
		{model: &SubscriptionOrder{}, table: "subscription_orders", name: "idx_subscription_orders_user_create", columns: "(user_id, create_time, id)"},
		{model: &SubscriptionOrder{}, table: "subscription_orders", name: "idx_subscription_orders_status_create", columns: "(status, create_time, id)", mysqlColumns: "(status(32), create_time, id)"},
		{model: &SubscriptionOrder{}, table: "subscription_orders", name: "idx_subscription_orders_status_complete", columns: "(status, complete_time, id)", mysqlColumns: "(status(32), complete_time, id)"},
	}

	for _, index := range indexes {
		if err := ensureRechargeOrderIndex(index.model, index.table, index.name, index.columns, index.mysqlColumns); err != nil {
			return err
		}
	}
	return nil
}

func ensureRechargeOrderIndex(model interface{}, table string, name string, columns string, mysqlColumns string) error {
	if !DB.Migrator().HasTable(model) || DB.Migrator().HasIndex(model, name) {
		return nil
	}
	if common.UsingMySQL && mysqlColumns != "" {
		columns = mysqlColumns
	}

	var stmt string
	switch {
	case common.UsingPostgreSQL:
		stmt = fmt.Sprintf("CREATE INDEX CONCURRENTLY IF NOT EXISTS %s ON %s %s", name, table, columns)
	case common.UsingSQLite:
		stmt = fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s %s", name, table, columns)
	default:
		stmt = fmt.Sprintf("CREATE INDEX %s ON %s %s", name, table, columns)
	}
	execDB := DB
	if common.UsingPostgreSQL {
		execDB = DB.Session(&gorm.Session{SkipDefaultTransaction: true})
	}
	if err := execDB.Exec(stmt).Error; err != nil {
		return fmt.Errorf("failed to create index %s: %w", name, err)
	}
	return nil
}
