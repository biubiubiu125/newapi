package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	prefillGroupNameMaxRunes        = 64
	prefillGroupTypeMaxRunes        = 32
	prefillGroupDescriptionMaxRunes = 255
)

var (
	ErrPrefillGroupNameTypeEmpty      = errors.New("prefill group name and type cannot be empty")
	ErrPrefillGroupNameTooLong        = errors.New("prefill group name is too long")
	ErrPrefillGroupTypeTooLong        = errors.New("prefill group type is too long")
	ErrPrefillGroupDescriptionTooLong = errors.New("prefill group description is too long")
)

// PrefillGroup 用于存储可复用的“组”信息，例如模型组、标签组、端点组等。
// Name 字段保持唯一，用于在前端下拉框中展示。
// Type 字段用于区分组的类别，可选值如：model、tag、endpoint。
// Items 字段使用 JSON 数组保存对应类型的字符串集合，示例：
// ["gpt-4o", "gpt-3.5-turbo"]
// 设计遵循 3NF，避免冗余，提供灵活扩展能力。

// JSONValue 基于 json.RawMessage 实现，支持从数据库的 []byte 和 string 两种类型读取
type JSONValue json.RawMessage

// Value 实现 driver.Valuer 接口，用于数据库写入
// 必须返回 string 而非 []byte:PG simple protocol 下 []byte 按 bytea 编码,
// 写 json 列会触发 SQLSTATE 22P02。
func (j JSONValue) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return string(j), nil
}

// Scan 实现 sql.Scanner 接口，兼容不同驱动返回的类型
func (j *JSONValue) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*j = nil
		return nil
	case []byte:
		// 拷贝底层字节，避免保留底层缓冲区
		b := make([]byte, len(v))
		copy(b, v)
		*j = JSONValue(b)
		return nil
	case string:
		*j = JSONValue([]byte(v))
		return nil
	default:
		// 其他类型尝试序列化为 JSON
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		*j = JSONValue(b)
		return nil
	}
}

// MarshalJSON 确保在对外编码时与 json.RawMessage 行为一致
func (j JSONValue) MarshalJSON() ([]byte, error) {
	if j == nil {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON 确保在对外解码时与 json.RawMessage 行为一致
func (j *JSONValue) UnmarshalJSON(data []byte) error {
	if data == nil {
		*j = nil
		return nil
	}
	b := make([]byte, len(data))
	copy(b, data)
	*j = JSONValue(b)
	return nil
}

type PrefillGroup struct {
	Id          int            `json:"id"`
	Name        string         `json:"name" gorm:"size:64;not null;uniqueIndex:uk_prefill_name,where:deleted_at IS NULL"`
	Type        string         `json:"type" gorm:"size:32;index;not null"`
	Items       JSONValue      `json:"items" gorm:"type:json"`
	Description string         `json:"description,omitempty" gorm:"type:varchar(255)"`
	CreatedTime int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

// NormalizePrefillGroup trims fields and enforces the PostgreSQL column limits.
func NormalizePrefillGroup(g *PrefillGroup) error {
	if g == nil {
		return ErrPrefillGroupNameTypeEmpty
	}
	g.Name = strings.TrimSpace(g.Name)
	g.Type = strings.TrimSpace(g.Type)
	g.Description = strings.TrimSpace(g.Description)
	if g.Name == "" || g.Type == "" {
		return ErrPrefillGroupNameTypeEmpty
	}
	if utf8.RuneCountInString(g.Name) > prefillGroupNameMaxRunes {
		return ErrPrefillGroupNameTooLong
	}
	if utf8.RuneCountInString(g.Type) > prefillGroupTypeMaxRunes {
		return ErrPrefillGroupTypeTooLong
	}
	if utf8.RuneCountInString(g.Description) > prefillGroupDescriptionMaxRunes {
		return ErrPrefillGroupDescriptionTooLong
	}
	return nil
}

// Insert 新建组
func (g *PrefillGroup) Insert() error {
	if err := NormalizePrefillGroup(g); err != nil {
		return err
	}
	now := common.GetTimestamp()
	g.CreatedTime = now
	g.UpdatedTime = now
	return DB.Create(g).Error
}

// IsPrefillGroupNameDuplicated 检查组名称是否重复（排除自身 ID）
func IsPrefillGroupNameDuplicated(id int, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&PrefillGroup{}).Where("name = ? AND id <> ?", name, id).Count(&cnt).Error
	return cnt > 0, err
}

// Update 更新组。只写可编辑列，保留创建时间，不复活已删除行。
func (g *PrefillGroup) Update() error {
	if err := NormalizePrefillGroup(g); err != nil {
		return err
	}
	if g.Id == 0 {
		return gorm.ErrRecordNotFound
	}
	var current PrefillGroup
	if err := DB.First(&current, g.Id).Error; err != nil {
		return err
	}
	now := common.GetTimestamp()
	result := DB.Model(&PrefillGroup{}).Where("id = ?", current.Id).Updates(map[string]any{
		"name":         g.Name,
		"type":         g.Type,
		"items":        g.Items,
		"description":  g.Description,
		"updated_time": now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	g.CreatedTime = current.CreatedTime
	g.UpdatedTime = now
	return nil
}

// DeleteByID 根据 ID 删除组
func DeletePrefillGroupByID(id int) error {
	return DB.Delete(&PrefillGroup{}, id).Error
}

// GetAllPrefillGroups 获取全部组，可按类型过滤（为空则返回全部）
func GetAllPrefillGroups(groupType string) ([]*PrefillGroup, error) {
	var groups []*PrefillGroup
	query := DB.Model(&PrefillGroup{})
	if groupType != "" {
		query = query.Where("type = ?", groupType)
	}
	if err := query.Order("updated_time DESC").Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}
