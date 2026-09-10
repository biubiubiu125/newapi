package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskPluginChannelRef struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

func GetTaskPluginUsage(key string) ([]TaskPluginChannelRef, int64, error) {
	var channels []Channel
	if err := DB.Where("type = ? AND status = ?", constant.ChannelTypeTaskPlugin, common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return nil, 0, err
	}
	refs := make([]TaskPluginChannelRef, 0)
	for _, channel := range channels {
		if channel.GetSetting().TaskPluginKey == key {
			refs = append(refs, TaskPluginChannelRef{Id: channel.Id, Name: channel.Name})
		}
	}
	var inFlight int64
	err := DB.Model(&Task{}).Where("platform = ? AND status NOT IN ?", key, []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).Count(&inFlight).Error
	return refs, inFlight, err
}

// TaskPluginVersionInUse reports whether any persisted task still points at
// this exact plugin version. Historical task polling and artifact rendering
// require that source row to remain available.
func TaskPluginVersionInUse(key, version string) (bool, error) {
	return taskPluginVersionInUse(DB, key, version)
}

func taskPluginVersionInUse(db *gorm.DB, key, version string) (bool, error) {
	key = strings.TrimSpace(key)
	version = strings.TrimSpace(version)
	if db == nil || key == "" || version == "" {
		return false, nil
	}
	var tasks []Task
	if err := db.Select("platform", "private_data").Where("platform = ?", key).Find(&tasks).Error; err != nil {
		return false, err
	}
	for _, task := range tasks {
		if task.PrivateData.Execution != nil &&
			task.PrivateData.Execution.TaskPlugin != nil &&
			task.PrivateData.Execution.TaskPlugin.Version == version {
			return true, nil
		}
	}
	return false, nil
}

type TaskPlugin struct {
	Id            int64   `json:"id"`
	Key           string  `json:"key" gorm:"size:128;not null;uniqueIndex:uk_task_plugin_key_version,priority:1"`
	APIVersion    int     `json:"api_version" gorm:"not null"`
	Version       string  `json:"version" gorm:"size:64;not null;uniqueIndex:uk_task_plugin_key_version,priority:2"`
	Source        string  `json:"source" gorm:"type:text;not null"`
	SourceHash    string  `json:"source_hash" gorm:"size:64;not null"`
	Enabled       bool    `json:"enabled" gorm:"not null"`
	Active        bool    `json:"active" gorm:"not null;index"`
	ActiveKey     *string `json:"-" gorm:"size:128;uniqueIndex:uk_task_plugin_active_key"`
	CreatedAt     int64   `json:"created_at" gorm:"not null"`
	Remark        string  `json:"remark" gorm:"type:text"`
	IconMediaType string  `json:"-" gorm:"size:32"`
	IconData      []byte  `json:"-"`
}

func (plugin *TaskPlugin) HasIcon() bool {
	return plugin != nil && plugin.IconMediaType != "" && len(plugin.IconData) > 0
}

var taskPluginKeyLocks sync.Map

var ErrTaskPluginVersionInUse = errors.New("task plugin version is still referenced by persisted tasks")

func withTaskPluginKeyLock(key string, fn func() error) error {
	lockValue, _ := taskPluginKeyLocks.LoadOrStore(key, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	return fn()
}

func WithTaskPluginKeyLock(key string, fn func() error) error {
	return withTaskPluginKeyLock(key, fn)
}

func saveTaskPluginLocked(plugin *TaskPlugin) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var existing TaskPlugin
		err := tx.Where(&TaskPlugin{Key: plugin.Key, Version: plugin.Version}).First(&existing).Error
		if err == nil {
			if existing.SourceHash != plugin.SourceHash {
				return errors.New("plugin key and version already exist with different source")
			}
			if err = tx.Model(&existing).Updates(map[string]any{"enabled": plugin.Enabled, "remark": plugin.Remark}).Error; err != nil {
				return err
			}
			existing.Enabled = plugin.Enabled
			existing.Remark = plugin.Remark
			if existing.Active && existing.ActiveKey == nil {
				activeKey := existing.Key
				if err = tx.Model(&existing).Update("active_key", activeKey).Error; err != nil {
					return err
				}
				existing.ActiveKey = &activeKey
			}
			*plugin = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		plugin.CreatedAt = time.Now().Unix()
		var count int64
		if err = tx.Model(&TaskPlugin{}).Where(&TaskPlugin{Key: plugin.Key, Active: true}).Count(&count).Error; err != nil {
			return err
		}
		plugin.Active = count == 0
		if plugin.Active {
			activeKey := plugin.Key
			plugin.ActiveKey = &activeKey
		}
		return tx.Create(plugin).Error
	})
}

func SaveTaskPlugin(plugin *TaskPlugin) error {
	if plugin == nil || strings.TrimSpace(plugin.Key) == "" || strings.TrimSpace(plugin.Version) == "" {
		return errors.New("task plugin identity is required")
	}
	return withTaskPluginKeyLock(plugin.Key, func() error {
		return saveTaskPluginLocked(plugin)
	})
}

func SaveTaskPluginLocked(plugin *TaskPlugin) error {
	if plugin == nil || strings.TrimSpace(plugin.Key) == "" || strings.TrimSpace(plugin.Version) == "" {
		return errors.New("task plugin identity is required")
	}
	return saveTaskPluginLocked(plugin)
}

func ensureTaskPluginActiveKeys() error {
	var plugins []TaskPlugin
	if err := DB.Order("key ASC, created_at DESC, id DESC").Find(&plugins).Error; err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		seen := make(map[string]bool)
		for index := range plugins {
			plugin := &plugins[index]
			if !plugin.Active {
				if plugin.ActiveKey != nil {
					if err := tx.Model(plugin).Update("active_key", nil).Error; err != nil {
						return err
					}
				}
				continue
			}
			if seen[plugin.Key] {
				if err := tx.Model(plugin).Updates(map[string]any{"active": false, "active_key": nil}).Error; err != nil {
					return err
				}
				continue
			}
			seen[plugin.Key] = true
			if plugin.ActiveKey == nil || *plugin.ActiveKey != plugin.Key {
				if err := tx.Model(plugin).Update("active_key", plugin.Key).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func ListTaskPluginVersions(key string) ([]TaskPlugin, error) {
	var plugins []TaskPlugin
	err := DB.Where(&TaskPlugin{Key: key}).Order("created_at DESC, id DESC").Find(&plugins).Error
	return plugins, err
}

func restoreTaskPluginVersionsLocked(key string, versions []TaskPlugin) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(&TaskPlugin{Key: key}).Delete(&TaskPlugin{}).Error; err != nil {
			return err
		}
		for i := len(versions) - 1; i >= 0; i-- {
			plugin := versions[i]
			if strings.TrimSpace(plugin.Key) == "" {
				plugin.Key = key
			}
			if plugin.Key != key {
				return fmt.Errorf("task plugin restore key mismatch: %q", plugin.Key)
			}
			if err := tx.Create(&plugin).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func RestoreTaskPluginVersions(key string, versions []TaskPlugin) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("task plugin key is required")
	}
	return withTaskPluginKeyLock(key, func() error {
		return restoreTaskPluginVersionsLocked(key, versions)
	})
}

func RestoreTaskPluginVersionsLocked(key string, versions []TaskPlugin) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("task plugin key is required")
	}
	return restoreTaskPluginVersionsLocked(key, versions)
}

func ListTaskPlugins() ([]TaskPlugin, error) {
	var plugins []TaskPlugin
	err := DB.
		Order(clause.OrderByColumn{Column: clause.Column{Name: "key"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "created_at"}, Desc: true}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}, Desc: true}).
		Find(&plugins).Error
	return plugins, err
}

func GetTaskPluginVersion(key, version string) (*TaskPlugin, error) {
	var plugin TaskPlugin
	query := DB.Where(&TaskPlugin{Key: key})
	if version == "" {
		query = query.Where(&TaskPlugin{Active: true})
	} else {
		query = query.Where(&TaskPlugin{Version: version})
	}
	if err := query.First(&plugin).Error; err != nil {
		return nil, err
	}
	return &plugin, nil
}

func ListActiveTaskPlugins() ([]TaskPlugin, error) {
	snapshot, err := GetTaskPluginSyncSnapshot()
	return snapshot.Plugins, err
}

type TaskPluginSyncSnapshot struct {
	Plugins  []TaskPlugin
	Revision string
}

// GetTaskPluginSyncSnapshot returns the enabled override set together with a
// deterministic revision of every active database override. Nodes can compare
// the revision even though their local routing-generation counters differ.
func GetTaskPluginSyncSnapshot() (TaskPluginSyncSnapshot, error) {
	var activePlugins []TaskPlugin
	if err := DB.Where(&TaskPlugin{Active: true}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "key"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "version"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}}).
		Find(&activePlugins).Error; err != nil {
		return TaskPluginSyncSnapshot{}, err
	}

	type revisionEntry struct {
		Key        string `json:"key"`
		APIVersion int    `json:"api_version"`
		Version    string `json:"version"`
		SourceHash string `json:"source_hash"`
		Enabled    bool   `json:"enabled"`
	}
	entries := make([]revisionEntry, 0, len(activePlugins))
	enabledPlugins := make([]TaskPlugin, 0, len(activePlugins))
	for _, plugin := range activePlugins {
		entries = append(entries, revisionEntry{
			Key:        plugin.Key,
			APIVersion: plugin.APIVersion,
			Version:    plugin.Version,
			SourceHash: plugin.SourceHash,
			Enabled:    plugin.Enabled,
		})
		if plugin.Enabled {
			enabledPlugins = append(enabledPlugins, plugin)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Key != entries[j].Key {
			return entries[i].Key < entries[j].Key
		}
		return entries[i].Version < entries[j].Version
	})
	payload, err := common.Marshal(entries)
	if err != nil {
		return TaskPluginSyncSnapshot{}, err
	}
	digest := sha256.Sum256(payload)
	return TaskPluginSyncSnapshot{
		Plugins:  enabledPlugins,
		Revision: hex.EncodeToString(digest[:]),
	}, nil
}

func activateTaskPluginLocked(key, version string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var target TaskPlugin
		if err := tx.Where(&TaskPlugin{Key: key, Version: version}).First(&target).Error; err != nil {
			return err
		}
		if err := tx.Model(&TaskPlugin{}).Where(&TaskPlugin{Key: key}).Updates(map[string]any{"active": false, "active_key": nil}).Error; err != nil {
			return err
		}
		return tx.Model(&target).Updates(map[string]any{"active": true, "active_key": key, "enabled": true}).Error
	})
}

func ActivateTaskPlugin(key, version string) error {
	return withTaskPluginKeyLock(key, func() error {
		return activateTaskPluginLocked(key, version)
	})
}

func ActivateTaskPluginLocked(key, version string) error {
	return activateTaskPluginLocked(key, version)
}

func setTaskPluginEnabledLocked(key string, enabled bool) error {
	result := DB.Model(&TaskPlugin{}).Where(&TaskPlugin{Key: key, Active: true}).Update("enabled", enabled)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func SetTaskPluginEnabled(key string, enabled bool) error {
	return withTaskPluginKeyLock(key, func() error {
		return setTaskPluginEnabledLocked(key, enabled)
	})
}

func SetTaskPluginEnabledLocked(key string, enabled bool) error {
	return setTaskPluginEnabledLocked(key, enabled)
}

type TaskPluginDeleteResult struct {
	DeletedActive bool
	Promoted      *TaskPlugin
}

func deleteTaskPluginVersionLocked(key, version string) (TaskPluginDeleteResult, error) {
	result := TaskPluginDeleteResult{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var plugin TaskPlugin
		if err := lockForUpdate(tx).Where(&TaskPlugin{Key: key, Version: version}).First(&plugin).Error; err != nil {
			return err
		}
		inUse, err := taskPluginVersionInUse(tx, key, version)
		if err != nil {
			return err
		}
		if inUse {
			return ErrTaskPluginVersionInUse
		}
		result.DeletedActive = plugin.Active
		if err := tx.Delete(&plugin).Error; err != nil {
			return err
		}
		if !plugin.Active {
			return nil
		}

		var promoted TaskPlugin
		err = lockForUpdate(tx).
			Where(&TaskPlugin{Key: key}).
			Order("created_at DESC, id DESC").
			First(&promoted).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = tx.Model(&promoted).Update("active", true).Error; err != nil {
			return err
		}
		promoted.Active = true
		activeKey := promoted.Key
		if err = tx.Model(&promoted).Update("active_key", activeKey).Error; err != nil {
			return err
		}
		promoted.ActiveKey = &activeKey
		result.Promoted = &promoted
		return nil
	})
	return result, err
}

func DeleteTaskPluginVersion(key, version string) (TaskPluginDeleteResult, error) {
	result := TaskPluginDeleteResult{}
	err := withTaskPluginKeyLock(key, func() error {
		var deleteErr error
		result, deleteErr = deleteTaskPluginVersionLocked(key, version)
		return deleteErr
	})
	return result, err
}

func DeleteTaskPluginVersionLocked(key, version string) (TaskPluginDeleteResult, error) {
	return deleteTaskPluginVersionLocked(key, version)
}
