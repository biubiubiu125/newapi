package model

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Channel struct {
	Id                 int     `json:"id"`
	Type               int     `json:"type" gorm:"default:0"`
	Key                string  `json:"key" gorm:"not null"`
	OpenAIOrganization *string `json:"openai_organization" gorm:"column:openai_organization"`
	TestModel          *string `json:"test_model"`
	Status             int     `json:"status" gorm:"default:1"`
	Name               string  `json:"name" gorm:"index"`
	Weight             *uint   `json:"weight" gorm:"default:0"`
	CreatedTime        int64   `json:"created_time" gorm:"bigint"`
	TestTime           int64   `json:"test_time" gorm:"bigint"`
	ResponseTime       int     `json:"response_time"` // in milliseconds
	BaseURL            *string `json:"base_url" gorm:"column:base_url;default:''"`
	Other              string  `json:"other"`
	Balance            float64 `json:"balance"` // in USD
	BalanceUpdatedTime int64   `json:"balance_updated_time" gorm:"bigint"`
	Models             string  `json:"models"`
	Group              string  `json:"group" gorm:"type:varchar(64);default:'default'"`
	UsedQuota          int64   `json:"used_quota" gorm:"bigint;default:0"`
	ModelMapping       *string `json:"model_mapping" gorm:"type:text"`
	//MaxInputTokens     *int    `json:"max_input_tokens" gorm:"default:0"`
	StatusCodeMapping *string `json:"status_code_mapping" gorm:"type:varchar(1024);default:''"`
	Priority          *int64  `json:"priority" gorm:"bigint;default:0"`
	AutoBan           *int    `json:"auto_ban" gorm:"default:1"`
	OtherInfo         string  `json:"other_info"`
	Tag               *string `json:"tag" gorm:"index"`
	Setting           *string `json:"setting" gorm:"type:text"` // 渠道额外设置
	ParamOverride     *string `json:"param_override" gorm:"type:text"`
	HeaderOverride    *string `json:"header_override" gorm:"type:text"`
	Remark            *string `json:"remark" gorm:"type:varchar(255)" validate:"max=255"`
	// add after v0.8.5
	ChannelInfo ChannelInfo `json:"channel_info" gorm:"type:json"`

	OtherSettings string `json:"settings" gorm:"column:settings"` // 其他设置，存储azure版本等不需要检索的信息，详见dto.ChannelOtherSettings

	// cache info
	Keys []string `json:"-" gorm:"-"`

	// keyStatusClearIndexes is removed from the stored multi-key maps after the
	// snapshot is merged. A missing key in the snapshot means "this writer did
	// not touch it", not "enable it".
	keyStatusClearIndexes []int `json:"-" gorm:"-"`
	// keyStatusReplaceMaps writes the snapshot maps as the full new state.
	// Enabling every key and deleting a key both mean that. The polling cursor
	// stays on the stored row.
	keyStatusReplaceMaps bool `json:"-" gorm:"-"`
	// keyStatusWriteStructure copies multi-key size, mode, and the multi-key
	// flag from this snapshot. Status saves must not roll those back.
	keyStatusWriteStructure bool `json:"-" gorm:"-"`
}

const ChannelStatusReasonAllKeysDisabled = "All keys are disabled"

type ChannelInfo struct {
	IsMultiKey             bool                  `json:"is_multi_key"`                        // 是否多Key模式
	MultiKeySize           int                   `json:"multi_key_size"`                      // 多Key模式下的Key数量
	MultiKeyStatusList     map[int]int           `json:"multi_key_status_list"`               // key状态列表，key index -> status
	MultiKeyDisabledReason map[int]string        `json:"multi_key_disabled_reason,omitempty"` // key禁用原因列表，key index -> reason
	MultiKeyDisabledTime   map[int]int64         `json:"multi_key_disabled_time,omitempty"`   // key禁用时间列表，key index -> time
	MultiKeyPollingIndex   int                   `json:"multi_key_polling_index"`             // 多Key模式下轮询的key索引
	MultiKeyMode           constant.MultiKeyMode `json:"multi_key_mode"`
}

type ChannelSortOptions struct {
	SortBy    string
	SortOrder string
	IDSort    bool
}

var channelSortColumns = map[string]string{
	"id":            "id",
	"name":          "name",
	"priority":      "priority",
	"balance":       "balance",
	"response_time": "response_time",
	"test_time":     "test_time",
}

func NewChannelSortOptions(sortBy string, sortOrder string, idSort bool) ChannelSortOptions {
	normalizedSortBy := strings.ToLower(strings.TrimSpace(sortBy))
	normalizedSortOrder := strings.ToLower(strings.TrimSpace(sortOrder))
	if _, ok := channelSortColumns[normalizedSortBy]; !ok {
		normalizedSortBy = ""
		normalizedSortOrder = ""
	} else if normalizedSortOrder != "asc" {
		normalizedSortOrder = "desc"
	}

	return ChannelSortOptions{
		SortBy:    normalizedSortBy,
		SortOrder: normalizedSortOrder,
		IDSort:    idSort,
	}
}

func (options ChannelSortOptions) Apply(query *gorm.DB) *gorm.DB {
	if columnName, ok := channelSortColumns[options.SortBy]; ok {
		return query.Order(clause.OrderByColumn{
			Column: clause.Column{Name: columnName},
			Desc:   options.SortOrder != "asc",
		})
	}
	if options.IDSort {
		return query.Order(clause.OrderByColumn{
			Column: clause.Column{Name: "id"},
			Desc:   true,
		})
	}
	return query.Order(clause.OrderByColumn{
		Column: clause.Column{Name: "priority"},
		Desc:   true,
	})
}

func resolveChannelSortOptions(idSort bool, sortOptions []ChannelSortOptions) ChannelSortOptions {
	if len(sortOptions) == 0 {
		return NewChannelSortOptions("", "", idSort)
	}
	options := sortOptions[0]
	options.IDSort = options.IDSort || idSort
	return options
}

func NormalizeChannelGroupFilter(group string) string {
	group = strings.TrimSpace(group)
	if group == "" || strings.EqualFold(group, "all") || strings.EqualFold(group, "null") {
		return ""
	}
	return group
}

func channelGroupFilterCondition() string {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return `CONCAT(',', ` + commonGroupCol + `, ',') LIKE ? ESCAPE '!'`
	}
	return `(',' || ` + commonGroupCol + ` || ',') LIKE ? ESCAPE '!'`
}

func channelGroupFilterPattern(group string) string {
	group = strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	).Replace(group)
	return "%," + group + ",%"
}

func ApplyChannelGroupFilter(query *gorm.DB, group string) *gorm.DB {
	group = NormalizeChannelGroupFilter(group)
	if group == "" {
		return query
	}
	return query.Where(channelGroupFilterCondition(), channelGroupFilterPattern(group))
}

// Value implements driver.Valuer interface
// 必须返回 string 而非 []byte:PG simple protocol 下 []byte 参数按 bytea
// 编码,写 json 列会触发 SQLSTATE 22P02。
func (c ChannelInfo) Value() (driver.Value, error) {
	data, err := common.Marshal(&c)
	if err != nil {
		return nil, err
	}
	// PostgreSQL JSON columns require a text JSON parameter when pgx runs in
	// simple protocol mode. Returning []byte makes it a binary parameter.
	return string(data), nil
}

// Scan implements sql.Scanner interface
func (c *ChannelInfo) Scan(value interface{}) error {
	bytesValue := jsonScanBytes(value)
	if len(bytesValue) == 0 {
		return nil
	}
	return common.Unmarshal(bytesValue, c)
}

func parseChannelKeyList(keys string) []string {
	if keys == "" {
		return []string{}
	}
	trimmed := strings.TrimSpace(keys)
	if strings.HasPrefix(trimmed, "[") {
		var arr []interface{}
		if err := common.Unmarshal([]byte(trimmed), &arr); err == nil {
			res := make([]string, 0, len(arr))
			for _, item := range arr {
				var key string
				switch value := item.(type) {
				case nil:
					continue
				case string:
					key = strings.TrimSpace(value)
				default:
					bytes, err := common.Marshal(value)
					if err != nil {
						continue
					}
					key = string(bytes)
				}
				if key != "" {
					res = append(res, key)
				}
			}
			return res
		}
	}
	parts := strings.Split(strings.Trim(keys, "\n"), "\n")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
}

func (channel *Channel) GetKeys() []string {
	if channel.Key == "" {
		return []string{}
	}
	if len(channel.Keys) > 0 {
		return channel.Keys
	}
	return parseChannelKeyList(channel.Key)
}

func (channel *Channel) GetNextEnabledKey() (string, int, *types.NewAPIError) {
	// If not in multi-key mode, return the original key string directly.
	if !channel.ChannelInfo.IsMultiKey {
		return channel.Key, 0, nil
	}

	lock := GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()

	// Polling, random, and the default mode all used to trust the caller's
	// status map. That map can say every key is disabled while the database
	// still has an enabled key, or the reverse, until the next cache sync.
	current := channel
	if channel.Id > 0 {
		fresh, err := GetChannelById(channel.Id, true)
		if err != nil {
			return "", 0, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		current = fresh
	}
	keys := current.GetKeys()
	if len(keys) == 0 {
		return "", 0, types.NewError(errors.New("no keys available"), types.ErrorCodeChannelNoAvailableKey)
	}
	statusList := current.ChannelInfo.MultiKeyStatusList
	// helper to get key status, default to enabled when missing
	getStatus := func(idx int) int {
		if statusList == nil {
			return common.ChannelStatusEnabled
		}
		if status, ok := statusList[idx]; ok {
			return status
		}
		return common.ChannelStatusEnabled
	}

	// Collect indexes of enabled keys. Blank lines are kept in the stored list
	// so existing status indexes do not shift, but they are not credentials.
	enabledIdx := make([]int, 0, len(keys))
	for i, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if getStatus(i) == common.ChannelStatusEnabled {
			enabledIdx = append(enabledIdx, i)
		}
	}
	// If no specific status list or none enabled, return an explicit error so caller can
	// properly handle a channel with no available keys (e.g. mark channel disabled).
	// Returning the first key here caused requests to keep using an already-disabled key.
	if len(enabledIdx) == 0 {
		return "", 0, types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)
	}

	switch current.ChannelInfo.MultiKeyMode {
	case constant.MultiKeyModeRandom:
		// Randomly pick one enabled key
		selectedIdx := enabledIdx[rand.Intn(len(enabledIdx))]
		return keys[selectedIdx], selectedIdx, nil
	case constant.MultiKeyModePolling:
		// The caller can hold a channel_info snapshot from before this lock.
		// Selecting and saving that snapshot puts a just-disabled key back into
		// the database. Read the current row under the lock and persist only that
		// copy, with the polling cursor advanced.
		fresh, err := channelForKeyPolling(channel.Id)
		if err != nil {
			return "", 0, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		freshKeys := fresh.GetKeys()
		if len(freshKeys) == 0 {
			return "", 0, types.NewError(errors.New("no keys available"), types.ErrorCodeChannelNoAvailableKey)
		}
		start := fresh.ChannelInfo.MultiKeyPollingIndex
		if start < 0 || start >= len(freshKeys) {
			start = 0
		}
		for i := range freshKeys {
			idx := (start + i) % len(freshKeys)
			if strings.TrimSpace(freshKeys[idx]) == "" {
				continue
			}
			if multiKeyStatus(fresh.ChannelInfo.MultiKeyStatusList, idx) != common.ChannelStatusEnabled {
				continue
			}
			nextIndex := (idx + 1) % len(freshKeys)
			fresh.ChannelInfo.MultiKeyPollingIndex = nextIndex
			channel.ChannelInfo.MultiKeyPollingIndex = nextIndex
			// Persist only the cursor. The in-memory channel can be a stale
			// snapshot, and saving it would put a disabled key back into the row.
			if saveErr := persistMultiKeyPollingIndex(channel.Id, nextIndex); saveErr != nil {
				common.SysLog(fmt.Sprintf("failed to save polling index: channel_id=%d, error=%v", channel.Id, saveErr))
			}
			if common.DebugEnabled {
				logger.LogDebug(nil, "channel %d polling index: %d", channel.Id, nextIndex)
			}
			return freshKeys[idx], idx, nil
		}
		return "", 0, types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)
	default:
		// Unknown mode, default to first enabled key (or original key string)
		return keys[enabledIdx[0]], enabledIdx[0], nil
	}
}

// ResolveReusableKey returns a previously stored credential when remix/polling
// must hit the same upstream account. An empty preferred key falls back to the
// normal enabled-key rotation used for new requests.
func (channel *Channel) ResolveReusableKey(preferredKey string) (string, int, *types.NewAPIError) {
	preferredKey = strings.TrimSpace(preferredKey)
	if preferredKey == "" {
		return channel.GetNextEnabledKey()
	}
	for i, key := range channel.GetKeys() {
		trimmed := strings.TrimSpace(key)
		if trimmed == preferredKey {
			return trimmed, i, nil
		}
	}
	return preferredKey, 0, nil
}

func (channel *Channel) SaveChannelInfo() error {
	return DB.Model(channel).Update("channel_info", channel.ChannelInfo).Error
}

// persistMultiKeyPollingIndex writes only the polling cursor. Replacing the
// whole channel_info document lets this process put a key back that another
// process has already disabled. The in-process polling lock does not cover
// that race. Callers still hold GetChannelPollingLock for the local cache.
func persistMultiKeyPollingIndex(channelID int, nextIndex int) error {
	return persistMultiKeyPollingIndexWithTx(DB, channelID, nextIndex)
}

func persistMultiKeyPollingIndexWithTx(tx *gorm.DB, channelID int, nextIndex int) error {
	if channelID <= 0 {
		return errors.New("channel ID is 0")
	}
	if tx == nil {
		tx = DB
	}
	result := tx.Model(&Channel{}).
		Where("id = ? AND "+multiKeyChannelInfoCondition(), channelID).
		Update("channel_info", pollingIndexUpdateExpr(nextIndex))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("channel %d not found", channelID)
	}
	return nil
}

// shiftMultiKeyPollingIndex keeps the next poll on the same surviving key.
// Deleting an earlier index moves that key left, so the stored cursor has to
// move with it. Deleting the cursor key itself leaves the cursor in place,
// because the following key slides into that index.
func shiftMultiKeyPollingIndex(cursor int, removed []int, newLen int) int {
	if newLen <= 0 {
		return 0
	}
	if cursor < 0 {
		cursor = 0
	}
	removedBefore := 0
	for _, index := range removed {
		if index < cursor {
			removedBefore++
		}
	}
	cursor -= removedBefore
	if cursor >= newLen {
		return 0
	}
	return cursor
}

func multiKeyChannelInfoCondition() string {
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		return `(channel_info::jsonb ->> 'is_multi_key') = 'true'`
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		return `JSON_UNQUOTE(JSON_EXTRACT(channel_info, '$.is_multi_key')) = 'true'`
	default:
		return `json_extract(channel_info, '$.is_multi_key') = 1`
	}
}

func pollingIndexUpdateExpr(nextIndex int) clause.Expr {
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		return gorm.Expr(
			"jsonb_set(channel_info::jsonb, '{multi_key_polling_index}', to_jsonb(?::bigint), true)::json",
			nextIndex,
		)
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		return gorm.Expr(
			"JSON_SET(channel_info, '$.multi_key_polling_index', ?)",
			nextIndex,
		)
	default:
		return gorm.Expr(
			"json_set(channel_info, '$.multi_key_polling_index', ?)",
			nextIndex,
		)
	}
}

// MarkMultiKeyStatusCleared records keys that must be removed from the stored
// maps. Call it when enabling one key; leaving the key out of the snapshot is
// not enough, because a merge keeps stored disables the writer did not load.
func (channel *Channel) MarkMultiKeyStatusCleared(indexes ...int) {
	if channel == nil {
		return
	}
	channel.keyStatusClearIndexes = append(channel.keyStatusClearIndexes, indexes...)
}

// MarkMultiKeyStatusReplaced marks the snapshot maps as the complete new
// status. Keys missing from the snapshot are enabled. The polling cursor is
// left untouched.
func (channel *Channel) MarkMultiKeyStatusReplaced() {
	if channel == nil {
		return
	}
	channel.keyStatusReplaceMaps = true
}

// MarkMultiKeyStructureReplaced marks a key deletion. The size, mode, and
// status maps in the snapshot replace the stored ones. The polling cursor
// still stays on the row.
func (channel *Channel) MarkMultiKeyStructureReplaced() {
	if channel == nil {
		return
	}
	channel.keyStatusReplaceMaps = true
	channel.keyStatusWriteStructure = true
}

// channelInfoUpdate builds a channel_info expression that merges or replaces
// only the multi-key status maps. The polling cursor is not part of the
// expression, so a concurrent poll cannot be rewound.
func (channel *Channel) channelInfoUpdate() (any, bool, error) {
	if channel.keyStatusReplaceMaps || channel.keyStatusWriteStructure {
		expr, err := channelInfoReplacingKeyStatus(channel)
		return expr, err == nil, err
	}
	patch, ok, err := multiKeyMergePatch(channel)
	if err != nil || !ok {
		return nil, false, err
	}
	return channelInfoMergingKeyStatus(patch), true, nil
}

func multiKeyMergePatch(channel *Channel) (string, bool, error) {
	status := multiKeyPatchMap(channel.ChannelInfo.MultiKeyStatusList)
	reason := multiKeyPatchMap(channel.ChannelInfo.MultiKeyDisabledReason)
	disabledAt := multiKeyPatchMap(channel.ChannelInfo.MultiKeyDisabledTime)
	for _, index := range channel.keyStatusClearIndexes {
		if index < 0 {
			continue
		}
		key := strconv.Itoa(index)
		status[key] = nil
		reason[key] = nil
		disabledAt[key] = nil
	}
	patch := map[string]any{}
	if len(status) > 0 {
		patch["multi_key_status_list"] = status
	}
	if len(reason) > 0 {
		patch["multi_key_disabled_reason"] = reason
	}
	if len(disabledAt) > 0 {
		patch["multi_key_disabled_time"] = disabledAt
	}
	if len(patch) == 0 {
		return "", false, nil
	}
	encoded, err := common.Marshal(patch)
	if err != nil {
		return "", false, err
	}
	return string(encoded), true, nil
}

func multiKeyPatchMap[V any](values map[int]V) map[string]any {
	patch := make(map[string]any, len(values))
	for index, value := range values {
		patch[strconv.Itoa(index)] = value
	}
	return patch
}

func channelInfoMergingKeyStatus(patch string) clause.Expr {
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		return gorm.Expr(postgresMergeKeyStatusSQL, patch, patch, patch, patch, patch, patch)
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		return gorm.Expr("JSON_MERGE_PATCH(COALESCE(channel_info, JSON_OBJECT()), CAST(? AS JSON))", patch)
	default:
		return gorm.Expr("json_patch(COALESCE(channel_info, '{}'), ?)", patch)
	}
}

const postgresMergeKeyStatusSQL = `(
jsonb_set(
  jsonb_set(
    jsonb_set(
      COALESCE(channel_info::jsonb, '{}'::jsonb),
      '{multi_key_status_list}',
      jsonb_strip_nulls(
        CASE WHEN jsonb_typeof(channel_info::jsonb->'multi_key_status_list') = 'object'
          THEN channel_info::jsonb->'multi_key_status_list' ELSE '{}'::jsonb END
        || CASE WHEN jsonb_typeof(?::jsonb->'multi_key_status_list') = 'object'
          THEN ?::jsonb->'multi_key_status_list' ELSE '{}'::jsonb END
      ),
      true
    ),
    '{multi_key_disabled_reason}',
    jsonb_strip_nulls(
      CASE WHEN jsonb_typeof(channel_info::jsonb->'multi_key_disabled_reason') = 'object'
        THEN channel_info::jsonb->'multi_key_disabled_reason' ELSE '{}'::jsonb END
      || CASE WHEN jsonb_typeof(?::jsonb->'multi_key_disabled_reason') = 'object'
        THEN ?::jsonb->'multi_key_disabled_reason' ELSE '{}'::jsonb END
    ),
    true
  ),
  '{multi_key_disabled_time}',
  jsonb_strip_nulls(
    CASE WHEN jsonb_typeof(channel_info::jsonb->'multi_key_disabled_time') = 'object'
      THEN channel_info::jsonb->'multi_key_disabled_time' ELSE '{}'::jsonb END
    || CASE WHEN jsonb_typeof(?::jsonb->'multi_key_disabled_time') = 'object'
      THEN ?::jsonb->'multi_key_disabled_time' ELSE '{}'::jsonb END
  ),
  true
)
)::json`

func channelInfoReplacingKeyStatus(channel *Channel) (clause.Expr, error) {
	status, err := jsonIntKeyMap(channel.ChannelInfo.MultiKeyStatusList)
	if err != nil {
		return clause.Expr{}, err
	}
	reason, err := jsonIntKeyMap(channel.ChannelInfo.MultiKeyDisabledReason)
	if err != nil {
		return clause.Expr{}, err
	}
	disabledAt, err := jsonIntKeyMap(channel.ChannelInfo.MultiKeyDisabledTime)
	if err != nil {
		return clause.Expr{}, err
	}
	isMulti := "false"
	if channel.ChannelInfo.IsMultiKey {
		isMulti = "true"
	}
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		if channel.keyStatusWriteStructure {
			return gorm.Expr(postgresReplaceKeyStatusAndStructureSQL,
				status, reason, disabledAt,
				channel.ChannelInfo.MultiKeySize, isMulti, string(channel.ChannelInfo.MultiKeyMode),
			), nil
		}
		return gorm.Expr(postgresReplaceKeyStatusSQL, status, reason, disabledAt), nil
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		if channel.keyStatusWriteStructure {
			return gorm.Expr(
				"JSON_SET(COALESCE(channel_info, JSON_OBJECT()), '$.multi_key_status_list', CAST(? AS JSON), '$.multi_key_disabled_reason', CAST(? AS JSON), '$.multi_key_disabled_time', CAST(? AS JSON), '$.multi_key_size', ?, '$.is_multi_key', CAST(? AS JSON), '$.multi_key_mode', ?)",
				status, reason, disabledAt, channel.ChannelInfo.MultiKeySize, isMulti, string(channel.ChannelInfo.MultiKeyMode),
			), nil
		}
		return gorm.Expr(
			"JSON_SET(COALESCE(channel_info, JSON_OBJECT()), '$.multi_key_status_list', CAST(? AS JSON), '$.multi_key_disabled_reason', CAST(? AS JSON), '$.multi_key_disabled_time', CAST(? AS JSON))",
			status, reason, disabledAt,
		), nil
	default:
		if channel.keyStatusWriteStructure {
			return gorm.Expr(
				"json_set(COALESCE(channel_info, '{}'), '$.multi_key_status_list', json(?), '$.multi_key_disabled_reason', json(?), '$.multi_key_disabled_time', json(?), '$.multi_key_size', ?, '$.is_multi_key', json(?), '$.multi_key_mode', ?)",
				status, reason, disabledAt, channel.ChannelInfo.MultiKeySize, isMulti, string(channel.ChannelInfo.MultiKeyMode),
			), nil
		}
		return gorm.Expr(
			"json_set(COALESCE(channel_info, '{}'), '$.multi_key_status_list', json(?), '$.multi_key_disabled_reason', json(?), '$.multi_key_disabled_time', json(?))",
			status, reason, disabledAt,
		), nil
	}
}

const postgresReplaceKeyStatusSQL = `(
jsonb_set(
  jsonb_set(
    jsonb_set(
      COALESCE(channel_info::jsonb, '{}'::jsonb),
      '{multi_key_status_list}', ?::jsonb, true
    ),
    '{multi_key_disabled_reason}', ?::jsonb, true
  ),
  '{multi_key_disabled_time}', ?::jsonb, true
)
)::json`

const postgresReplaceKeyStatusAndStructureSQL = `(
jsonb_set(
  jsonb_set(
    jsonb_set(
      jsonb_set(
        jsonb_set(
          jsonb_set(
            COALESCE(channel_info::jsonb, '{}'::jsonb),
            '{multi_key_status_list}', ?::jsonb, true
          ),
          '{multi_key_disabled_reason}', ?::jsonb, true
        ),
        '{multi_key_disabled_time}', ?::jsonb, true
      ),
      '{multi_key_size}', to_jsonb(?::int), true
    ),
    '{is_multi_key}', ?::jsonb, true
  ),
  '{multi_key_mode}', to_jsonb(?::text), true
)
)::json`

func jsonIntKeyMap[V any](values map[int]V) (string, error) {
	encoded := make(map[string]V, len(values))
	for index, value := range values {
		encoded[strconv.Itoa(index)] = value
	}
	data, err := common.Marshal(encoded)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// channelForKeyPolling returns the row loaded after the caller takes
// GetChannelPollingLock. Memory cache can still list a key another process
// has already disabled, so selection does not trust that object.
func channelForKeyPolling(channelID int) (*Channel, error) {
	if channelID <= 0 {
		return nil, errors.New("channel ID is 0")
	}
	channel, err := GetChannelById(channelID, true)
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return nil, fmt.Errorf("channel %d not found", channelID)
	}
	return channel, nil
}

func multiKeyStatus(statusList map[int]int, idx int) int {
	if statusList == nil {
		return common.ChannelStatusEnabled
	}
	if status, ok := statusList[idx]; ok {
		return status
	}
	return common.ChannelStatusEnabled
}

func (channel *Channel) GetModels() []string {
	if channel.Models == "" {
		return []string{}
	}
	return common.SplitCommaSeparated(channel.Models)
}

func (channel *Channel) GetGroups() []string {
	if channel.Group == "" {
		return []string{}
	}
	return common.SplitCommaSeparated(channel.Group)
}

func (channel *Channel) GetOtherInfo() map[string]any {
	otherInfo := make(map[string]any)
	if channel.OtherInfo != "" {
		err := common.Unmarshal([]byte(channel.OtherInfo), &otherInfo)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		}
	}
	return otherInfo
}

func (channel *Channel) SetOtherInfo(otherInfo map[string]interface{}) {
	otherInfoBytes, err := common.Marshal(otherInfo)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		return
	}
	channel.OtherInfo = string(otherInfoBytes)
}

func (channel *Channel) GetTag() string {
	if channel.Tag == nil {
		return ""
	}
	return *channel.Tag
}

func (channel *Channel) SetTag(tag string) {
	channel.Tag = &tag
}

func (channel *Channel) GetAutoBan() bool {
	if channel.AutoBan == nil {
		return false
	}
	return *channel.AutoBan == 1
}

func (channel *Channel) Save() error {
	return DB.Save(channel).Error
}

// saveStatusState persists only the fields owned by the channel status flow.
// Keeping this allowlist here prevents a stale channel snapshot from
// overwriting credentials, accounting counters, or channel configuration.
func (channel *Channel) saveStatusState() error {
	return channel.saveStatusStateWithTx(DB)
}

func (channel *Channel) saveStatusStateWithTx(tx *gorm.DB) error {
	if channel.Id == 0 {
		return errors.New("channel ID is 0")
	}
	if tx == nil {
		tx = DB
	}
	updates := map[string]any{
		"status":     channel.Status,
		"other_info": channel.OtherInfo,
	}
	if channel.ChannelInfo.IsMultiKey || channel.keyStatusReplaceMaps || channel.keyStatusWriteStructure {
		expr, include, err := channel.channelInfoUpdate()
		if err != nil {
			return err
		}
		if include {
			updates["channel_info"] = expr
		}
	}
	return tx.Model(&Channel{}).Where("id = ?", channel.Id).Updates(updates).Error
}

// SaveKeyManagementState persists a multi-key admin change without writing
// accounting columns. A full struct Update would put the in-memory used_quota,
// balance, and response time back over concurrent billing.
var (
	ErrMultiKeyIndexOutOfRange  = errors.New("multi-key index out of range")
	ErrCannotDeleteLastMultiKey = errors.New("cannot delete the last multi-key")
	ErrNoAutoDisabledMultiKey   = errors.New("no auto-disabled multi-key")
	ErrNoDisableableMultiKey    = errors.New("no disableable multi-key")
	errChannelRoutingUnchanged  = errors.New("channel routing unchanged")
)

func lockChannelForUpdate(tx *gorm.DB, channelID int) (*Channel, error) {
	if channelID <= 0 {
		return nil, errors.New("channel ID is 0")
	}
	var channel Channel
	err := LockForUpdate(tx).First(&channel, "id = ?", channelID).Error
	if err != nil {
		return nil, err
	}
	return &channel, nil
}

func withLockedChannel(channelID int, fn func(tx *gorm.DB, channel *Channel) error) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		channel, err := lockChannelForUpdate(tx, channelID)
		if err != nil {
			return err
		}
		return fn(tx, channel)
	})
}

func (channel *Channel) SaveKeyManagementState(includeKey bool) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		return channel.saveKeyManagementStateWithTx(tx, includeKey)
	})
}

func (channel *Channel) saveKeyManagementStateWithTx(tx *gorm.DB, includeKey bool) error {
	if channel == nil || channel.Id == 0 {
		return errors.New("channel ID is 0")
	}
	expr, include, err := channel.channelInfoUpdate()
	if err != nil {
		return err
	}
	updates := map[string]any{
		"status":     channel.Status,
		"other_info": channel.OtherInfo,
	}
	if include {
		updates["channel_info"] = expr
	}
	if includeKey {
		updates["key"] = channel.Key
	}
	if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).Updates(updates).Error; err != nil {
		return err
	}
	return channel.reloadAndUpdateAbilitiesWithTx(tx)
}

func DisableMultiKey(channelID int, keyIndex int) error {
	return withLockedChannel(channelID, func(tx *gorm.DB, channel *Channel) error {
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			return ErrMultiKeyIndexOutOfRange
		}
		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime == nil {
			channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason == nil {
			channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		}
		now := common.GetTimestamp()
		channel.ChannelInfo.MultiKeyStatusList[keyIndex] = common.ChannelStatusManuallyDisabled
		channel.ChannelInfo.MultiKeyDisabledTime[keyIndex] = now
		channel.ChannelInfo.MultiKeyDisabledReason[keyIndex] = "manual disable"
		SyncChannelStatusWithEnabledKeys(channel, "key disabled")
		channel.ChannelInfo.MultiKeyStatusList = map[int]int{keyIndex: common.ChannelStatusManuallyDisabled}
		channel.ChannelInfo.MultiKeyDisabledTime = map[int]int64{keyIndex: now}
		channel.ChannelInfo.MultiKeyDisabledReason = map[int]string{keyIndex: "manual disable"}
		return channel.saveKeyManagementStateWithTx(tx, false)
	})
}

func EnableMultiKey(channelID int, keyIndex int) error {
	return withLockedChannel(channelID, func(tx *gorm.DB, channel *Channel) error {
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			return ErrMultiKeyIndexOutOfRange
		}
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime != nil {
			delete(channel.ChannelInfo.MultiKeyDisabledTime, keyIndex)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason != nil {
			delete(channel.ChannelInfo.MultiKeyDisabledReason, keyIndex)
		}
		SyncChannelStatusWithEnabledKeys(channel, "")
		channel.ChannelInfo.MultiKeyStatusList = nil
		channel.ChannelInfo.MultiKeyDisabledTime = nil
		channel.ChannelInfo.MultiKeyDisabledReason = nil
		channel.MarkMultiKeyStatusCleared(keyIndex)
		return channel.saveKeyManagementStateWithTx(tx, false)
	})
}

func EnableAllMultiKeys(channelID int) (int, error) {
	enabledCount := 0
	err := withLockedChannel(channelID, func(tx *gorm.DB, channel *Channel) error {
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			enabledCount = len(channel.ChannelInfo.MultiKeyStatusList)
		}
		channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		SyncChannelStatusWithEnabledKeys(channel, "")
		channel.MarkMultiKeyStatusReplaced()
		return channel.saveKeyManagementStateWithTx(tx, false)
	})
	return enabledCount, err
}

func DisableAllMultiKeys(channelID int) (int, error) {
	disabledCount := 0
	err := withLockedChannel(channelID, func(tx *gorm.DB, channel *Channel) error {
		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime == nil {
			channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason == nil {
			channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		}
		now := common.GetTimestamp()
		for i := 0; i < channel.ChannelInfo.MultiKeySize; i++ {
			status := common.ChannelStatusEnabled
			if stored, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists {
				status = stored
			}
			if status != common.ChannelStatusEnabled {
				continue
			}
			channel.ChannelInfo.MultiKeyStatusList[i] = common.ChannelStatusManuallyDisabled
			channel.ChannelInfo.MultiKeyDisabledTime[i] = now
			channel.ChannelInfo.MultiKeyDisabledReason[i] = "manual disable"
			disabledCount++
		}
		if disabledCount == 0 {
			return ErrNoDisableableMultiKey
		}
		SyncChannelStatusWithEnabledKeys(channel, "all keys disabled")
		return channel.saveKeyManagementStateWithTx(tx, false)
	})
	return disabledCount, err
}

// DeleteMultiKeyAtIndex removes one key and reindexes status maps from the
// row locked in this transaction. A disable that committed first is applied to
// the surviving key's new index instead of the index from before the delete.
func DeleteMultiKeyAtIndex(channelID int, keyIndex int) error {
	return withLockedChannel(channelID, func(tx *gorm.DB, channel *Channel) error {
		if err := reindexWithoutKey(channel, keyIndex); err != nil {
			return err
		}
		cursor := channel.ChannelInfo.MultiKeyPollingIndex
		if err := channel.saveKeyManagementStateWithTx(tx, true); err != nil {
			return err
		}
		return persistMultiKeyPollingIndexWithTx(tx, channel.Id, cursor)
	})
}

func DeleteAutoDisabledMultiKeys(channelID int) (int, error) {
	deletedCount := 0
	err := withLockedChannel(channelID, func(tx *gorm.DB, channel *Channel) error {
		keys := channel.GetKeys()
		remaining := make([]string, 0, len(keys))
		newStatus := make(map[int]int)
		newTime := make(map[int]int64)
		newReason := make(map[int]string)
		removed := make([]int, 0)
		next := 0
		cursor := channel.ChannelInfo.MultiKeyPollingIndex
		for i, key := range keys {
			status := common.ChannelStatusEnabled
			if channel.ChannelInfo.MultiKeyStatusList != nil {
				if stored, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists {
					status = stored
				}
			}
			if status == common.ChannelStatusAutoDisabled {
				deletedCount++
				removed = append(removed, i)
				continue
			}
			remaining = append(remaining, key)
			if status != common.ChannelStatusEnabled {
				newStatus[next] = status
				if channel.ChannelInfo.MultiKeyDisabledTime != nil {
					if disabledAt, exists := channel.ChannelInfo.MultiKeyDisabledTime[i]; exists {
						newTime[next] = disabledAt
					}
				}
				if channel.ChannelInfo.MultiKeyDisabledReason != nil {
					if reason, exists := channel.ChannelInfo.MultiKeyDisabledReason[i]; exists {
						newReason[next] = reason
					}
				}
			}
			next++
		}
		if deletedCount == 0 {
			return ErrNoAutoDisabledMultiKey
		}
		applyReindexedKeys(channel, remaining, newStatus, newTime, newReason, "disabled keys deleted")
		nextCursor := shiftMultiKeyPollingIndex(cursor, removed, len(remaining))
		channel.ChannelInfo.MultiKeyPollingIndex = nextCursor
		if err := channel.saveKeyManagementStateWithTx(tx, true); err != nil {
			return err
		}
		return persistMultiKeyPollingIndexWithTx(tx, channel.Id, nextCursor)
	})
	return deletedCount, err
}

func reindexWithoutKey(channel *Channel, keyIndex int) error {
	keys := channel.GetKeys()
	if keyIndex < 0 || keyIndex >= len(keys) {
		return ErrMultiKeyIndexOutOfRange
	}
	cursor := channel.ChannelInfo.MultiKeyPollingIndex
	remaining := make([]string, 0, len(keys)-1)
	newStatus := make(map[int]int)
	newTime := make(map[int]int64)
	newReason := make(map[int]string)
	next := 0
	for i, key := range keys {
		if i == keyIndex {
			continue
		}
		remaining = append(remaining, key)
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			if status, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists && status != common.ChannelStatusEnabled {
				newStatus[next] = status
			}
		}
		if channel.ChannelInfo.MultiKeyDisabledTime != nil {
			if disabledAt, exists := channel.ChannelInfo.MultiKeyDisabledTime[i]; exists {
				newTime[next] = disabledAt
			}
		}
		if channel.ChannelInfo.MultiKeyDisabledReason != nil {
			if reason, exists := channel.ChannelInfo.MultiKeyDisabledReason[i]; exists {
				newReason[next] = reason
			}
		}
		next++
	}
	if len(remaining) == 0 {
		return ErrCannotDeleteLastMultiKey
	}
	applyReindexedKeys(channel, remaining, newStatus, newTime, newReason, "key deleted")
	channel.ChannelInfo.MultiKeyPollingIndex = shiftMultiKeyPollingIndex(cursor, []int{keyIndex}, len(remaining))
	return nil
}

func applyReindexedKeys(channel *Channel, keys []string, status map[int]int, disabledAt map[int]int64, reason map[int]string, syncReason string) {
	channel.Key = strings.Join(keys, "\n")
	channel.Keys = nil
	channel.ChannelInfo.MultiKeySize = len(keys)
	channel.ChannelInfo.MultiKeyStatusList = status
	channel.ChannelInfo.MultiKeyDisabledTime = disabledAt
	channel.ChannelInfo.MultiKeyDisabledReason = reason
	SyncChannelStatusWithEnabledKeys(channel, syncReason)
	channel.MarkMultiKeyStructureReplaced()
}

func GetAllChannels(startIdx int, num int, selectAll bool, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	var err error
	order := resolveChannelSortOptions(idSort, sortOptions)
	if selectAll {
		err = order.Apply(DB).Find(&channels).Error
	} else {
		err = order.Apply(DB).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	}
	return channels, err
}

func GetChannelsByTag(tag string, idSort bool, selectAll bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	order := resolveChannelSortOptions(idSort, sortOptions)
	query := order.Apply(DB.Where("tag = ?", tag))
	if !selectAll {
		query = query.Omit("key")
	}
	err := query.Find(&channels).Error
	return channels, err
}

func SearchChannels(keyword string, group string, model string, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	modelsCol := "`models`"

	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	order := resolveChannelSortOptions(idSort, sortOptions)

	// 构造基础查询
	baseQuery := DB.Model(&Channel{}).Omit("key")

	// 构造WHERE子句
	whereClause := "(id = ? OR name LIKE ? OR " + commonKeyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	// 执行查询
	err := order.Apply(baseQuery).Find(&channels).Error
	if err != nil {
		return nil, err
	}
	return channels, nil
}

// GetChannelById loads a channel directly from the database, bypassing the
// in-memory channel cache.
//
// WARNING: do NOT call this on request hot paths (middleware, distribution,
// relay submit/retry). Every call is a synchronous DB query and will not see
// cache-only state. Use CacheGetChannel instead: it serves from the in-memory
// cache and falls back to this function automatically when MemoryCacheEnabled
// is false. Direct use is appropriate only where fresh DB state is required,
// e.g. admin CRUD, channel testing, cache rebuilding, or choosing a multi-key
// credential. Key selection has to see a disable committed by another process
// before the next cache sync.
func GetChannelById(id int, selectAll bool) (*Channel, error) {
	channel := &Channel{Id: id}
	var err error = nil
	if selectAll {
		err = DB.First(channel, "id = ?", id).Error
	} else {
		err = DB.Omit("key").First(channel, "id = ?", id).Error
	}
	if err != nil {
		return nil, err
	}
	return channel, nil
}

func BatchInsertChannels(channels []Channel) error {
	if len(channels) == 0 {
		return nil
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	for _, chunk := range lo.Chunk(channels, 50) {
		for i := range chunk {
			chunk[i].Models = common.NormalizeCommaSeparated(chunk[i].Models)
			chunk[i].Group = common.NormalizeCommaSeparated(chunk[i].Group)
		}
		if err := tx.Create(&chunk).Error; err != nil {
			tx.Rollback()
			return err
		}
		for _, channel_ := range chunk {
			if err := channel_.AddAbilities(tx); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit().Error
}

func BatchDeleteChannels(ids []int) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	// 使用事务 分批删除channel表和abilities表
	tx := DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	var deletedCount int64
	for _, chunk := range lo.Chunk(ids, 200) {
		result := tx.Where("id in (?)", chunk).Delete(&Channel{})
		if result.Error != nil {
			tx.Rollback()
			return 0, result.Error
		}
		deletedCount += result.RowsAffected
		if err := tx.Where("channel_id in (?)", chunk).Delete(&Ability{}).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return deletedCount, nil
}

func (channel *Channel) GetPriority() int64 {
	if channel.Priority == nil {
		return 0
	}
	return *channel.Priority
}

func (channel *Channel) GetWeight() int {
	if channel.Weight == nil {
		return 0
	}
	return int(*channel.Weight)
}

func (channel *Channel) GetBaseURL() string {
	var baseURL string
	if channel.BaseURL != nil {
		baseURL = *channel.BaseURL
	}
	if strings.TrimSpace(baseURL) == "" &&
		channel.Type >= 0 &&
		channel.Type < len(constant.ChannelBaseURLs) {
		baseURL = constant.ChannelBaseURLs[channel.Type]
	}
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func (channel *Channel) GetModelMapping() string {
	if channel.ModelMapping == nil {
		return ""
	}
	return *channel.ModelMapping
}

func (channel *Channel) GetStatusCodeMapping() string {
	if channel.StatusCodeMapping == nil {
		return ""
	}
	return *channel.StatusCodeMapping
}

func (channel *Channel) Insert() error {
	channel.Models = common.NormalizeCommaSeparated(channel.Models)
	channel.Group = common.NormalizeCommaSeparated(channel.Group)
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(channel).Error; err != nil {
			return err
		}
		return channel.AddAbilities(tx)
	})
}

func (channel *Channel) prepareForUpdate() {
	channel.Models = common.NormalizeCommaSeparated(channel.Models)
	channel.Group = common.NormalizeCommaSeparated(channel.Group)
	// If this is a multi-key channel, recalculate MultiKeySize based on the current key list to avoid inconsistency after editing keys
	if channel.ChannelInfo.IsMultiKey {
		var keyStr string
		if channel.Key != "" {
			keyStr = channel.Key
		} else {
			// If key is not provided, read the existing key from the database
			if existing, err := GetChannelById(channel.Id, true); err == nil {
				keyStr = existing.Key
			}
		}
		// Parse the key list (supports newline separation or JSON array)
		keys := parseChannelKeyList(keyStr)
		channel.ChannelInfo.MultiKeySize = len(keys)
		// Clean up status data that exceeds the new key count to prevent index out of range
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			for idx := range channel.ChannelInfo.MultiKeyStatusList {
				if idx >= channel.ChannelInfo.MultiKeySize {
					delete(channel.ChannelInfo.MultiKeyStatusList, idx)
				}
			}
		}
		if channel.ChannelInfo.MultiKeyDisabledReason != nil {
			for idx := range channel.ChannelInfo.MultiKeyDisabledReason {
				if idx >= channel.ChannelInfo.MultiKeySize {
					delete(channel.ChannelInfo.MultiKeyDisabledReason, idx)
				}
			}
		}
		if channel.ChannelInfo.MultiKeyDisabledTime != nil {
			for idx := range channel.ChannelInfo.MultiKeyDisabledTime {
				if idx >= channel.ChannelInfo.MultiKeySize {
					delete(channel.ChannelInfo.MultiKeyDisabledTime, idx)
				}
			}
		}
	}
}

func (channel *Channel) reloadAndUpdateAbilities() error {
	if err := DB.Model(channel).First(channel, "id = ?", channel.Id).Error; err != nil {
		return err
	}
	return channel.UpdateAbilities(nil)
}

func (channel *Channel) reloadAndUpdateAbilitiesWithTx(tx *gorm.DB) error {
	if tx == nil {
		return channel.reloadAndUpdateAbilities()
	}
	if err := tx.Model(channel).First(channel, "id = ?", channel.Id).Error; err != nil {
		return err
	}
	return channel.UpdateAbilities(tx)
}

func (channel *Channel) Update() error {
	channel.prepareForUpdate()
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(channel).Updates(channel).Error; err != nil {
			return err
		}
		return channel.reloadAndUpdateAbilitiesWithTx(tx)
	})
}

func (channel *Channel) UpdateColumnsWithTx(tx *gorm.DB, updates map[string]any) error {
	if tx == nil {
		return channel.UpdateColumns(updates)
	}
	channel.prepareForUpdate()
	if len(updates) > 0 {
		if _, ok := updates["models"]; ok {
			updates["models"] = channel.Models
		}
		if _, ok := updates["group"]; ok {
			updates["group"] = channel.Group
		}
		if _, ok := updates["channel_info"]; ok {
			updates["channel_info"] = channel.ChannelInfo
		}
		if _, ok := updates["key"]; ok {
			updates["key"] = channel.Key
		}
		if err := tx.Model(channel).Updates(updates).Error; err != nil {
			return err
		}
	}
	return channel.reloadAndUpdateAbilitiesWithTx(tx)
}

func (channel *Channel) UpdateColumns(updates map[string]any) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		return channel.UpdateColumnsWithTx(tx, updates)
	})
}

func (channel *Channel) UpdateResponseTime(responseTime int64) {
	err := DB.Model(channel).Select("response_time", "test_time").Updates(Channel{
		TestTime:     common.GetTimestamp(),
		ResponseTime: int(responseTime),
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update response time: channel_id=%d, error=%v", channel.Id, err))
	}
}

func (channel *Channel) UpdateBalance(balance float64) {
	err := DB.Model(channel).Select("balance_updated_time", "balance").Updates(Channel{
		BalanceUpdatedTime: common.GetTimestamp(),
		Balance:            balance,
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update balance: channel_id=%d, error=%v", channel.Id, err))
	}
}

func (channel *Channel) Delete() error {
	if channel == nil || channel.Id == 0 {
		return errors.New("channel id is required")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(channel).Error; err != nil {
			return err
		}
		return tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	})
}

var channelStatusLock sync.Mutex

// channelPollingLocks stores locks for each channel.id to ensure thread-safe polling
var channelPollingLocks sync.Map

// GetChannelPollingLock returns or creates a mutex for the given channel ID
func GetChannelPollingLock(channelId int) *sync.Mutex {
	if lock, exists := channelPollingLocks.Load(channelId); exists {
		return lock.(*sync.Mutex)
	}
	// Create new lock for this channel
	newLock := &sync.Mutex{}
	actual, _ := channelPollingLocks.LoadOrStore(channelId, newLock)
	return actual.(*sync.Mutex)
}

// CleanupChannelPollingLocks removes locks for channels that no longer exist
// This is optional and can be called periodically to prevent memory leaks
func CleanupChannelPollingLocks() {
	var activeChannelIds []int
	DB.Model(&Channel{}).Pluck("id", &activeChannelIds)

	activeChannelSet := make(map[int]bool)
	for _, id := range activeChannelIds {
		activeChannelSet[id] = true
	}

	channelPollingLocks.Range(func(key, value any) bool {
		channelId := key.(int)
		if !activeChannelSet[channelId] {
			channelPollingLocks.Delete(channelId)
		}
		return true
	})
}

// preserveManualDisable keeps an operator disable in place. An in-flight
// auto-disable must not turn it into an auto-disable, or automatic recovery
// can switch the channel back on.
func preserveManualDisable(current, requested int) int {
	if current == common.ChannelStatusManuallyDisabled && requested == common.ChannelStatusAutoDisabled {
		return current
	}
	return requested
}

// keyedEnableReopensManualDisable is an automatic recovery or per-key enable.
// The operator status API passes an empty key and is allowed to turn the
// channel back on. A health check that still holds an older auto-disabled
// snapshot must not.
func keyedEnableReopensManualDisable(current int, usingKey string, requested int) bool {
	return current == common.ChannelStatusManuallyDisabled &&
		requested == common.ChannelStatusEnabled &&
		strings.TrimSpace(usingKey) != ""
}

func handlerMultiKeyUpdate(channel *Channel, usingKey string, status int, reason string) {
	keys := channel.GetKeys()
	if len(keys) == 0 {
		channel.Status = preserveManualDisable(channel.Status, status)
	} else {
		keyIndex := -1
		usingKey = strings.TrimSpace(usingKey)
		for i, key := range keys {
			if strings.TrimSpace(key) == usingKey {
				keyIndex = i
				break
			}
		}
		if keyIndex < 0 {
			if usingKey != "" {
				common.SysLog(fmt.Sprintf("failed to update multi-key status: channel_id=%d, using key not found", channel.Id))
				return
			}
			channel.Status = preserveManualDisable(channel.Status, status)
			if status == common.ChannelStatusEnabled {
				channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
				channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
				channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
			}
			if channel.Status == status {
				info := channel.GetOtherInfo()
				info["status_reason"] = reason
				info["status_time"] = common.GetTimestamp()
				channel.SetOtherInfo(info)
			}
			return
		}
		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if status == common.ChannelStatusEnabled {
			delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
			if channel.ChannelInfo.MultiKeyDisabledReason != nil {
				delete(channel.ChannelInfo.MultiKeyDisabledReason, keyIndex)
			}
			if channel.ChannelInfo.MultiKeyDisabledTime != nil {
				delete(channel.ChannelInfo.MultiKeyDisabledTime, keyIndex)
			}
		} else {
			channel.ChannelInfo.MultiKeyStatusList[keyIndex] = status
			if channel.ChannelInfo.MultiKeyDisabledReason == nil {
				channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
			}
			if channel.ChannelInfo.MultiKeyDisabledTime == nil {
				channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
			}
			channel.ChannelInfo.MultiKeyDisabledReason[keyIndex] = reason
			channel.ChannelInfo.MultiKeyDisabledTime[keyIndex] = common.GetTimestamp()
		}
		if !hasEnabledMultiKey(keys, channel.ChannelInfo.MultiKeyStatusList) {
			nextStatus := preserveManualDisable(channel.Status, common.ChannelStatusAutoDisabled)
			if nextStatus != channel.Status {
				channel.Status = nextStatus
				info := channel.GetOtherInfo()
				if reason != "" {
					info["status_reason"] = reason
				} else {
					info["status_reason"] = "All keys are disabled"
				}
				info["status_time"] = common.GetTimestamp()
				channel.SetOtherInfo(info)
			}
		} else if status == common.ChannelStatusEnabled && !keyedEnableReopensManualDisable(channel.Status, usingKey, status) {
			channel.Status = common.ChannelStatusEnabled
		}
	}
}

func hasEnabledMultiKey(keys []string, statusList map[int]int) bool {
	for i, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if statusList == nil {
			return true
		}
		status, ok := statusList[i]
		if !ok || status == common.ChannelStatusEnabled {
			return true
		}
	}
	return false
}

func SyncChannelStatusWithEnabledKeys(channel *Channel, reason string) {
	if channel == nil || !channel.ChannelInfo.IsMultiKey {
		return
	}
	// A manual or tag disable is an operator decision. Enabling a key may
	// restore a channel that was auto-disabled because every key was exhausted,
	// but it must not reopen one that an operator turned off.
	if channel.Status == common.ChannelStatusManuallyDisabled {
		return
	}
	if hasEnabledMultiKey(channel.GetKeys(), channel.ChannelInfo.MultiKeyStatusList) {
		if channel.Status != common.ChannelStatusEnabled {
			channel.Status = common.ChannelStatusEnabled
		}
		return
	}
	channel.Status = common.ChannelStatusAutoDisabled
	info := channel.GetOtherInfo()
	if reason == "" {
		reason = "All keys are disabled"
	}
	info["status_reason"] = reason
	info["status_time"] = common.GetTimestamp()
	channel.SetOtherInfo(info)
}

// prepareMultiKeyStatusWrite keeps a per-key update from rewriting every other
// key. The handler still computes the channel status from the full map first.
// After that, only the touched index is merged, or every map is replaced when
// the caller enabled the whole channel.
func prepareMultiKeyStatusWrite(channel *Channel, usingKey string, status int) {
	if channel == nil || !channel.ChannelInfo.IsMultiKey {
		return
	}
	if strings.TrimSpace(usingKey) == "" {
		if status == common.ChannelStatusEnabled {
			channel.keyStatusReplaceMaps = true
		}
		return
	}
	channel.limitStatusWriteToMultiKey(multiKeyIndex(channel, usingKey), status)
}

func multiKeyIndex(channel *Channel, usingKey string) int {
	usingKey = strings.TrimSpace(usingKey)
	if channel == nil || usingKey == "" {
		return -1
	}
	for index, key := range channel.GetKeys() {
		if strings.TrimSpace(key) == usingKey {
			return index
		}
	}
	return -1
}

func (channel *Channel) limitStatusWriteToMultiKey(keyIndex int, status int) {
	if channel == nil || keyIndex < 0 {
		return
	}
	if status == common.ChannelStatusEnabled {
		channel.keyStatusClearIndexes = append(channel.keyStatusClearIndexes, keyIndex)
		channel.ChannelInfo.MultiKeyStatusList = nil
		channel.ChannelInfo.MultiKeyDisabledReason = nil
		channel.ChannelInfo.MultiKeyDisabledTime = nil
		return
	}
	var reason string
	var disabledAt int64
	if channel.ChannelInfo.MultiKeyDisabledReason != nil {
		reason = channel.ChannelInfo.MultiKeyDisabledReason[keyIndex]
	}
	if channel.ChannelInfo.MultiKeyDisabledTime != nil {
		disabledAt = channel.ChannelInfo.MultiKeyDisabledTime[keyIndex]
	}
	disabledStatus := status
	if channel.ChannelInfo.MultiKeyStatusList != nil {
		if stored, ok := channel.ChannelInfo.MultiKeyStatusList[keyIndex]; ok {
			disabledStatus = stored
		}
	}
	channel.ChannelInfo.MultiKeyStatusList = map[int]int{keyIndex: disabledStatus}
	channel.ChannelInfo.MultiKeyDisabledReason = map[int]string{keyIndex: reason}
	channel.ChannelInfo.MultiKeyDisabledTime = map[int]int64{keyIndex: disabledAt}
}

func UpdateChannelStatus(channelId int, usingKey string, status int, reason string) bool {
	refreshReasonOnly := status == common.ChannelStatusAutoDisabled && strings.TrimSpace(reason) != ""
	if common.MemoryCacheEnabled {
		channelStatusLock.Lock()
		defer channelStatusLock.Unlock()
	}

	// Process lock first, then the database row. Tag updates lock rows in id
	// order and do not take this mutex, so the two orders cannot deadlock.
	pollingLock := GetChannelPollingLock(channelId)
	pollingLock.Lock()
	defer pollingLock.Unlock()

	err := DB.Transaction(func(tx *gorm.DB) error {
		channel, err := lockChannelForUpdate(tx, channelId)
		if err != nil {
			return err
		}
		write, updateAbilities, err := applyChannelStatusLocked(channel, usingKey, status, reason, refreshReasonOnly)
		if err != nil || !write {
			return err
		}
		if err := channel.saveStatusStateWithTx(tx); err != nil {
			return err
		}
		if updateAbilities {
			return updateAbilityEnabled(tx, channel.Id, channel.Status == common.ChannelStatusEnabled)
		}
		return nil
	})
	if errors.Is(err, errChannelRoutingUnchanged) || errors.Is(err, gorm.ErrRecordNotFound) {
		return false
	}
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update channel status: channel_id=%d, status=%d, error=%v", channelId, status, err))
		if common.MemoryCacheEnabled {
			InitChannelCache()
		}
		return false
	}
	if common.MemoryCacheEnabled {
		if fresh, readErr := GetChannelById(channelId, true); readErr == nil {
			copyChannelRoutingToCache(fresh)
		}
	}
	return true
}

func applyChannelStatusLocked(channel *Channel, usingKey string, status int, reason string, refreshReasonOnly bool) (bool, bool, error) {
	if status == common.ChannelStatusEnabled && strings.TrimSpace(usingKey) != "" && keyedEnableReopensManualDisable(channel.Status, usingKey, status) {
		return false, false, errChannelRoutingUnchanged
	}
	if channel.Status == status {
		if channel.ChannelInfo.IsMultiKey && strings.TrimSpace(usingKey) != "" {
			beforeStatus := channel.Status
			handlerMultiKeyUpdate(channel, usingKey, status, reason)
			prepareMultiKeyStatusWrite(channel, usingKey, status)
			return true, beforeStatus != channel.Status, nil
		}
		if !refreshReasonOnly {
			return false, false, errChannelRoutingUnchanged
		}
		info := channel.GetOtherInfo()
		info["status_reason"] = reason
		info["status_time"] = common.GetTimestamp()
		channel.SetOtherInfo(info)
		return true, false, nil
	}
	if channel.ChannelInfo.IsMultiKey {
		beforeStatus := channel.Status
		handlerMultiKeyUpdate(channel, usingKey, status, reason)
		prepareMultiKeyStatusWrite(channel, usingKey, status)
		return true, beforeStatus != channel.Status, nil
	}
	if keyedEnableReopensManualDisable(channel.Status, usingKey, status) || preserveManualDisable(channel.Status, status) != status {
		return false, false, errChannelRoutingUnchanged
	}
	info := channel.GetOtherInfo()
	info["status_reason"] = reason
	info["status_time"] = common.GetTimestamp()
	channel.SetOtherInfo(info)
	channel.Status = status
	return true, true, nil
}

func copyChannelRoutingToCache(channel *Channel) {
	if !common.MemoryCacheEnabled || channel == nil || channel.Id == 0 {
		return
	}
	cached, err := CacheGetChannel(channel.Id)
	if err != nil || cached == nil {
		return
	}
	before := cached.Status
	cached.Status = channel.Status
	cached.OtherInfo = channel.OtherInfo
	cached.Key = channel.Key
	cached.Keys = nil
	cached.ChannelInfo.IsMultiKey = channel.ChannelInfo.IsMultiKey
	cached.ChannelInfo.MultiKeySize = channel.ChannelInfo.MultiKeySize
	cached.ChannelInfo.MultiKeyMode = channel.ChannelInfo.MultiKeyMode
	cached.ChannelInfo.MultiKeyPollingIndex = channel.ChannelInfo.MultiKeyPollingIndex
	cached.ChannelInfo.MultiKeyStatusList = cloneIntIntMap(channel.ChannelInfo.MultiKeyStatusList)
	cached.ChannelInfo.MultiKeyDisabledReason = cloneIntStringMap(channel.ChannelInfo.MultiKeyDisabledReason)
	cached.ChannelInfo.MultiKeyDisabledTime = cloneIntInt64Map(channel.ChannelInfo.MultiKeyDisabledTime)
	if before != cached.Status {
		CacheUpdateChannelStatus(channel.Id, cached.Status)
	}
}

func cloneIntIntMap(src map[int]int) map[int]int {
	if src == nil {
		return nil
	}
	dst := make(map[int]int, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func cloneIntStringMap(src map[int]string) map[int]string {
	if src == nil {
		return nil
	}
	dst := make(map[int]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func cloneIntInt64Map(src map[int]int64) map[int]int64 {
	if src == nil {
		return nil
	}
	dst := make(map[int]int64, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func EnableChannelByTag(tag string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var channels []Channel
		if err := LockForUpdate(tx).Where("tag = ?", tag).Order("id asc").Find(&channels).Error; err != nil {
			return err
		}
		for _, channel := range channels {
			channel.Status = common.ChannelStatusEnabled
			if channel.ChannelInfo.IsMultiKey {
				channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
				channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
				channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
				channel.MarkMultiKeyStatusReplaced()
			}
			info := channel.GetOtherInfo()
			info["status_reason"] = "tag enabled"
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
			if err := channel.saveStatusStateWithTx(tx); err != nil {
				return err
			}
		}
		return updateAbilityStatusByTag(tx, tag, true)
	})
}

func DisableChannelByTag(tag string) error {
	// Explicit tag-level disable also cancels automatic restoration for
	// channels that were already disabled because all keys were unavailable.
	// Lock the rows before reading status so an in-flight per-key write cannot
	// commit a stale enabled snapshot over this manual disable.
	return DB.Transaction(func(tx *gorm.DB) error {
		var channels []Channel
		if err := LockForUpdate(tx).Where("tag = ?", tag).Order("id asc").Find(&channels).Error; err != nil {
			return err
		}
		for i := range channels {
			channel := &channels[i]
			if !channel.ChannelInfo.IsMultiKey || channel.GetOtherInfo()["status_reason"] != ChannelStatusReasonAllKeysDisabled {
				continue
			}
			// Already manual means the previous status write refused the change.
			// Keep that failure so a tag disable does not silently skip it.
			if channel.Status == common.ChannelStatusManuallyDisabled {
				return fmt.Errorf("failed to disable channel #%d by tag", channel.Id)
			}
			handlerMultiKeyUpdate(channel, "", common.ChannelStatusManuallyDisabled, "manual tag operation")
			if err := channel.saveStatusStateWithTx(tx); err != nil {
				return fmt.Errorf("failed to disable channel #%d by tag: %w", channel.Id, err)
			}
		}
		if err := tx.Model(&Channel{}).Where("tag = ?", tag).Update("status", common.ChannelStatusManuallyDisabled).Error; err != nil {
			return err
		}
		return updateAbilityStatusByTag(tx, tag, false)
	})
}

func EditChannelByTag(tag string, newTag *string, modelMapping *string, models *string, group *string, priority *int64, weight *uint, paramOverride *string, headerOverride *string) error {
	// A map keeps explicit empty values. An empty new tag dissolves the tag,
	// and an empty model list clears every model. GORM struct Updates skips both.
	updates := map[string]interface{}{}
	shouldReCreateAbilities := false
	if newTag != nil && *newTag != tag {
		updates["tag"] = *newTag
	}
	if modelMapping != nil {
		updates["model_mapping"] = *modelMapping
	}
	if models != nil {
		shouldReCreateAbilities = true
		updates["models"] = common.NormalizeCommaSeparated(*models)
	}
	if group != nil {
		// An empty group is a clear, matching an empty model list. Omitting the
		// field is the only way to leave groups unchanged.
		shouldReCreateAbilities = true
		updates["group"] = common.NormalizeCommaSeparated(*group)
	}
	if priority != nil {
		updates["priority"] = *priority
	}
	if weight != nil {
		updates["weight"] = *weight
	}
	if paramOverride != nil {
		updates["param_override"] = *paramOverride
	}
	if headerOverride != nil {
		updates["header_override"] = *headerOverride
	}

	if len(updates) == 0 && !shouldReCreateAbilities {
		return updateAbilityByTag(DB, tag, newTag, priority, weight)
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		// Lock the rows that currently have the tag, in the same id order as
		// enable and disable. A channel that joins after this read is not part
		// of the edit, and a disable that commits first is visible here.
		var channels []Channel
		if err := LockForUpdate(tx).Where("tag = ?", tag).Order("id asc").Omit("key").Find(&channels).Error; err != nil {
			return err
		}
		if len(channels) == 0 {
			return nil
		}
		channelIDs := make([]int, len(channels))
		for i := range channels {
			channelIDs[i] = channels[i].Id
		}
		if len(updates) > 0 {
			if err := tx.Model(&Channel{}).Where("id IN ?", channelIDs).Updates(updates).Error; err != nil {
				return err
			}
		}
		if !shouldReCreateAbilities {
			return updateAbilityByTagIDs(tx, channelIDs, tag, newTag, priority, weight)
		}
		for i := range channels {
			if err := channels[i].UpdateAbilities(tx); err != nil {
				return fmt.Errorf("failed to update abilities: channel_id=%d, tag=%s: %w", channels[i].Id, channels[i].GetTag(), err)
			}
		}
		return nil
	})
}

func UpdateChannelUsedQuota(id int, quota int) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeChannelUsedQuota, id, int64(quota))
		return
	}
	_ = updateChannelUsedQuota(id, quota)
}

func UpdateChannelUsedQuotaSync(id int, quota int) error {
	return updateChannelUsedQuota(id, quota)
}

func updateChannelUsedQuota(id int, quota int) error {
	if err := updateChannelUsedQuotaWithDB(DB, id, quota); err != nil {
		common.SysLog(fmt.Sprintf("failed to update channel used quota: channel_id=%d, delta_quota=%d, error=%v", id, quota, err))
		return err
	}
	return nil
}

var ErrChannelUsedQuotaNoRows = errors.New("channel used quota update failed")

func IsChannelUsedQuotaNoRowsError(err error) bool {
	return errors.Is(err, ErrChannelUsedQuotaNoRows)
}

func updateChannelUsedQuotaWithDB(db *gorm.DB, id int, quota int) error {
	if quota == 0 || id <= 0 {
		return nil
	}
	updateExpr := gorm.Expr("used_quota + ?", quota)
	if quota < 0 {
		updateExpr = gorm.Expr("CASE WHEN used_quota + ? < 0 THEN 0 ELSE used_quota + ? END", quota, quota)
	}
	result := db.Model(&Channel{}).Where("id = ?", id).Update("used_quota", updateExpr)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := db.Model(&Channel{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("%w, channel_id=%d, delta_quota=%d", ErrChannelUsedQuotaNoRows, id, quota)
		}
	}
	return nil
}

func DeleteChannelByStatus(status int64) (int64, error) {
	var ids []int
	err := DB.Model(&Channel{}).Where("status = ?", status).Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	return BatchDeleteChannels(ids)
}

func DeleteDisabledChannel() (int64, error) {
	var ids []int
	err := DB.Model(&Channel{}).Where("status = ? or status = ?", common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled).Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	return BatchDeleteChannels(ids)
}

func GetPaginatedTags(offset int, limit int) ([]*string, error) {
	return GetPaginatedChannelTags(DB.Model(&Channel{}), offset, limit)
}

func GetPaginatedChannelTags(query *gorm.DB, offset int, limit int) ([]*string, error) {
	var tags []*string
	err := query.
		Select("DISTINCT tag").
		Where("tag is not null AND tag != ''").
		Order(clause.OrderByColumn{Column: clause.Column{Name: "tag"}}).
		Offset(offset).
		Limit(limit).
		Find(&tags).Error
	return tags, err
}

func SearchTags(keyword string, group string, model string, idSort bool) ([]*string, error) {
	var tags []*string
	modelsCol := "`models`"

	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	order := "priority desc"
	if idSort {
		order = "id desc"
	}

	// 构造基础查询
	baseQuery := DB.Model(&Channel{}).Omit("key")

	// 构造WHERE子句
	whereClause := "(id = ? OR name LIKE ? OR " + commonKeyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	subQuery := baseQuery.
		Select("tag").
		Where("tag != ''").
		Order(order)

	err := DB.Table("(?) as sub", subQuery).
		Select("DISTINCT tag").
		Find(&tags).Error

	if err != nil {
		return nil, err
	}

	return tags, nil
}

func (channel *Channel) ValidateSettings() error {
	channelParams := &dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), channelParams)
		if err != nil {
			return err
		}
	}
	if _, err := common.ParseProxyURLStrict(channelParams.Proxy); err != nil {
		return fmt.Errorf("invalid channel proxy: %w", err)
	}
	if err := channelParams.ValidateHTTPTransport(); err != nil {
		return err
	}
	channelOtherSettings := &dto.ChannelOtherSettings{}
	if channel.OtherSettings != "" {
		err := common.UnmarshalJsonStr(channel.OtherSettings, channelOtherSettings)
		if err != nil {
			return err
		}
	}
	if err := channelOtherSettings.ValidateToolLossPolicy(); err != nil {
		return err
	}
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		if channelOtherSettings.AdvancedCustom == nil {
			return fmt.Errorf("advanced_custom is required")
		}
	}
	switch strings.TrimSpace(channelOtherSettings.ImageTaskMode) {
	case "", dto.ImageTaskModeSyncWrapper:
	default:
		return fmt.Errorf("image_task_mode is invalid")
	}
	if channelOtherSettings.AdvancedCustom != nil {
		if err := channelOtherSettings.AdvancedCustom.Validate(); err != nil {
			return err
		}
	}
	if constant.IsAdvancedCustomChannel(channel.Type) && channelOtherSettings.UpstreamModelUpdateCheckEnabled {
		if _, ok := channelOtherSettings.AdvancedCustom.ModelListRoute(); !ok {
			return fmt.Errorf("advanced custom channels require a %s route when upstream model update checks are enabled", dto.AdvancedCustomModelListPath)
		}
	}
	return nil
}

func (channel *Channel) GetSetting() dto.ChannelSettings {
	setting := dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), &setting)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal setting: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return setting
}

func (channel *Channel) SetSetting(setting dto.ChannelSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.Setting = common.GetPointer[string](string(settingBytes))
}

func (channel *Channel) GetOtherSettings() dto.ChannelOtherSettings {
	setting := dto.ChannelOtherSettings{}
	if channel.OtherSettings != "" {
		err := common.UnmarshalJsonStr(channel.OtherSettings, &setting)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal setting: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	if preset := common.GetAdvancedCustomPreset(channel.Type); preset != nil {
		setting.AdvancedCustom = preset
	}
	return setting
}

func (channel *Channel) SetOtherSettings(setting dto.ChannelOtherSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.OtherSettings = string(settingBytes)
}

func (channel *Channel) GetParamOverride() map[string]any {
	paramOverride := make(map[string]any)
	if channel.ParamOverride != nil && *channel.ParamOverride != "" {
		err := common.Unmarshal([]byte(*channel.ParamOverride), &paramOverride)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal param override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return paramOverride
}

func (channel *Channel) GetHeaderOverride() map[string]any {
	headerOverride := make(map[string]any)
	if channel.HeaderOverride != nil && *channel.HeaderOverride != "" {
		err := common.Unmarshal([]byte(*channel.HeaderOverride), &headerOverride)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal header override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return headerOverride
}

func GetChannelsByIds(ids []int) ([]*Channel, error) {
	var channels []*Channel
	err := DB.Where("id in (?)", ids).Find(&channels).Error
	return channels, err
}

func BatchSetChannelTag(ids []int, tag *string) error {
	// 开启事务
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	// 更新标签
	err := tx.Model(&Channel{}).Where("id in (?)", ids).Update("tag", tag).Error
	if err != nil {
		tx.Rollback()
		return err
	}

	// update ability status
	channels, err := GetChannelsByIds(ids)
	if err != nil {
		tx.Rollback()
		return err
	}

	for _, channel := range channels {
		err = channel.UpdateAbilities(tx)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	// 提交事务
	return tx.Commit().Error
}

// CountAllChannels returns total channels in DB
func CountAllChannels() (int64, error) {
	var total int64
	err := DB.Model(&Channel{}).Count(&total).Error
	return total, err
}

// CountAllTags returns number of non-empty distinct tags
func CountAllTags() (int64, error) {
	return CountChannelTags(DB.Model(&Channel{}))
}

func CountChannelTags(query *gorm.DB) (int64, error) {
	var total int64
	err := query.Where("tag is not null AND tag != ''").Distinct("tag").Count(&total).Error
	return total, err
}

// Get channels of specified type with pagination
func GetChannelsByType(startIdx int, num int, idSort bool, channelType int) ([]*Channel, error) {
	var channels []*Channel
	order := "priority desc"
	if idSort {
		order = "id desc"
	}
	err := DB.Where("type = ?", channelType).Order(order).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	return channels, err
}

// Count channels of specific type
func CountChannelsByType(channelType int) (int64, error) {
	var count int64
	err := DB.Model(&Channel{}).Where("type = ?", channelType).Count(&count).Error
	return count, err
}

// Return map[type]count for all channels
func CountChannelsGroupByType() (map[int64]int64, error) {
	type result struct {
		Type  int64 `gorm:"column:type"`
		Count int64 `gorm:"column:count"`
	}
	var results []result
	err := DB.Model(&Channel{}).Select("type, count(*) as count").Group("type").Find(&results).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int64)
	for _, r := range results {
		counts[r.Type] = r.Count
	}
	return counts, nil
}
