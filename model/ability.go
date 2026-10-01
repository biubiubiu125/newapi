package model

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Ability struct {
	Group     string  `json:"group" gorm:"type:varchar(64);primaryKey;autoIncrement:false"`
	Model     string  `json:"model" gorm:"type:varchar(255);primaryKey;autoIncrement:false"`
	ChannelId int     `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool    `json:"enabled"`
	Priority  *int64  `json:"priority" gorm:"bigint;default:0;index"`
	Weight    uint    `json:"weight" gorm:"default:0;index"`
	Tag       *string `json:"tag" gorm:"index"`
}

type AbilityWithChannel struct {
	Ability
	ChannelType int `json:"channel_type"`
}

func GetAllEnableAbilityWithChannels() ([]AbilityWithChannel, error) {
	var abilities []AbilityWithChannel
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type").
		Joins("left join channels on abilities.channel_id = channels.id").
		Where("abilities.enabled = ?", true).
		Scan(&abilities).Error
	return abilities, err
}

func GetGroupEnabledModels(group string) []string {
	var models []string
	group = strings.TrimSpace(group)
	// Find distinct models
	DB.Table("abilities").Where("TRIM("+commonGroupCol+") = ? and enabled = ?", group, true).Distinct("TRIM(model)").Pluck("TRIM(model)", &models)
	return models
}

func GetEnabledModels() []string {
	models, _ := GetEnabledModelsWithError()
	return models
}

func GetEnabledModelsWithError() ([]string, error) {
	var models []string
	// Find distinct models
	err := DB.Table("abilities").Where("enabled = ?", true).Distinct("TRIM(model)").Pluck("TRIM(model)", &models).Error
	return models, err
}

func GetAllEnableAbilities() []Ability {
	var abilities []Ability
	DB.Find(&abilities, "enabled = ?", true)
	return abilities
}

func GetChannel(group string, model string, retry int) (*Channel, error) {
	return GetChannelWithExclude(group, model, retry, nil, "")
}

func GetChannelWithExclude(group string, model string, retry int, excludeChannelIds []int, requestPath string) (*Channel, error) {
	return GetChannelWithExcludeAndFilter(group, model, retry, excludeChannelIds, requestPath, nil)
}

func GetChannelWithExcludeAndFilter(group string, model string, retry int, excludeChannelIds []int, requestPath string, channelFilter func(*Channel) bool) (*Channel, error) {
	var abilities []Ability

	var err error = nil
	if len(excludeChannelIds) > 0 {
		retry = 0
	}
	group = strings.TrimSpace(group)
	model = strings.TrimSpace(model)

	abilities, err = findPathFilteredAbilities(group, model, model, excludeChannelIds, requestPath, channelFilter)
	if err != nil {
		return nil, err
	}
	if len(abilities) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(model)
		if normalizedModel != "" && normalizedModel != model {
			abilities, err = findPathFilteredAbilities(group, normalizedModel, model, excludeChannelIds, requestPath, channelFilter)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(abilities) == 0 && len(excludeChannelIds) > 0 {
		return nil, nil
	}
	abilities = filterAbilitiesByRetryPriority(abilities, retry)
	if len(abilities) == 0 {
		return nil, nil
	}
	// Weight 0 gets no traffic while any positive weight exists, matching the
	// memory-cache picker. An all-zero tier is shared evenly.
	totalWeight := abilitySelectionWeightTotal(abilities)
	if totalWeight <= 0 {
		return nil, nil
	}
	chosen, ok := selectAbilityByWeight(abilities, common.GetRandomInt(totalWeight))
	if !ok {
		return nil, nil
	}
	channel := Channel{Id: chosen.ChannelId}
	err = DB.First(&channel, "id = ?", channel.Id).Error
	return &channel, err
}

func findPathFilteredAbilities(group string, queryModel string, requestModel string, excludeChannelIds []int, requestPath string, channelFilter func(*Channel) bool) ([]Ability, error) {
	var abilities []Ability
	channelQuery := DB.Where("TRIM("+commonGroupCol+") = ? and TRIM(model) = ? and enabled = ?", group, queryModel, true)
	if len(excludeChannelIds) > 0 {
		channelQuery = channelQuery.Where("channel_id NOT IN ?", excludeChannelIds)
	}
	if err := channelQuery.Order("weight DESC").Find(&abilities).Error; err != nil {
		return nil, err
	}
	filteredAbilities, err := filterAbilitiesByRequestPathAndModel(abilities, requestPath, requestModel)
	if err != nil {
		return nil, err
	}
	if channelFilter != nil {
		filteredAbilities, err = filterAbilitiesByChannelPredicate(filteredAbilities, channelFilter)
		if err != nil {
			return nil, err
		}
	}
	return filteredAbilities, nil
}

func filterAbilitiesByChannelPredicate(abilities []Ability, channelFilter func(*Channel) bool) ([]Ability, error) {
	if channelFilter == nil || len(abilities) == 0 {
		return abilities, nil
	}
	channelIDs := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		channelIDs = append(channelIDs, ability.ChannelId)
	}
	var channels []Channel
	if err := DB.Where("id IN ?", channelIDs).Find(&channels).Error; err != nil {
		return nil, err
	}
	channelsByID := make(map[int]*Channel, len(channels))
	for index := range channels {
		channelsByID[channels[index].Id] = &channels[index]
	}
	filtered := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		channel := channelsByID[ability.ChannelId]
		if channel != nil && channelFilter(channel) {
			filtered = append(filtered, ability)
		}
	}
	return filtered, nil
}

func filterAbilitiesByRetryPriority(abilities []Ability, retry int) []Ability {
	if len(abilities) == 0 {
		return abilities
	}

	uniquePriorities := make(map[int]struct{})
	for _, ability := range abilities {
		uniquePriorities[abilityPriorityValue(ability)] = struct{}{}
	}
	sortedPriorities := make([]int, 0, len(uniquePriorities))
	for priority := range uniquePriorities {
		sortedPriorities = append(sortedPriorities, priority)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sortedPriorities)))
	if retry >= len(sortedPriorities) {
		retry = len(sortedPriorities) - 1
	}
	targetPriority := sortedPriorities[retry]

	filtered := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		if abilityPriorityValue(ability) == targetPriority {
			filtered = append(filtered, ability)
		}
	}
	return filtered
}

func abilityPriorityValue(ability Ability) int {
	if ability.Priority == nil {
		return 0
	}
	return int(*ability.Priority)
}

func abilitySelectionWeights(abilities []Ability) []int {
	weights := make([]int, len(abilities))
	hasPositive := false
	for _, ability := range abilities {
		if ability.Weight > 0 {
			hasPositive = true
			break
		}
	}
	for i, ability := range abilities {
		weight := int(ability.Weight)
		if weight < 0 {
			weight = 0
		}
		if hasPositive {
			weights[i] = weight
			continue
		}
		weights[i] = 1
	}
	return weights
}

func abilitySelectionWeightTotal(abilities []Ability) int {
	total := 0
	for _, weight := range abilitySelectionWeights(abilities) {
		total += weight
	}
	return total
}

func selectAbilityByWeight(abilities []Ability, roll int) (Ability, bool) {
	weights := abilitySelectionWeights(abilities)
	total := 0
	for _, weight := range weights {
		total += weight
	}
	if total <= 0 {
		return Ability{}, false
	}
	if roll < 0 {
		roll = 0
	}
	roll %= total
	var last Ability
	found := false
	for i, ability := range abilities {
		if weights[i] <= 0 {
			continue
		}
		last = ability
		found = true
		roll -= weights[i]
		if roll < 0 {
			return ability, true
		}
	}
	return last, found
}

// filterAbilitiesByRequestPathAndModel restricts candidates by request path and
// model for the DB (non-memory-cache) selection path. Only Advanced Custom
// (type 58) channels are path-checked: kept only when one of their routes matches
// requestPath and model; all other channel types always pass. When requestPath is
// empty, filtering is skipped.
func filterAbilitiesByRequestPathAndModel(abilities []Ability, requestPath string, model string) ([]Ability, error) {
	if requestPath == "" || len(abilities) == 0 {
		return abilities, nil
	}

	channelIds := make([]int, 0, len(abilities))
	seen := make(map[int]struct{}, len(abilities))
	for _, ability := range abilities {
		if _, ok := seen[ability.ChannelId]; ok {
			continue
		}
		seen[ability.ChannelId] = struct{}{}
		channelIds = append(channelIds, ability.ChannelId)
	}

	var channels []*Channel
	if err := DB.Where("id IN ?", channelIds).Find(&channels).Error; err != nil {
		return nil, err
	}

	advancedConfigs := make(map[int]*kitdto.AdvancedCustomConfig)
	for _, channel := range channels {
		if channel.Type == constant.ChannelTypeAdvancedCustom {
			advancedConfigs[channel.Id] = channel.GetOtherSettings().AdvancedCustom
		}
	}

	filtered := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		config, isAdvancedCustom := advancedConfigs[ability.ChannelId]
		if !isAdvancedCustom {
			filtered = append(filtered, ability)
			continue
		}
		if config != nil && config.SupportsPathForModel(requestPath, model) {
			filtered = append(filtered, ability)
		}
	}
	return filtered, nil
}

func filterAbilitiesByConstraints(abilities []Ability, modelName string, filters []dto.ChannelFilter) []Ability {
	if len(abilities) == 0 {
		return nil
	}

	channelIds := make([]int, 0, len(abilities))
	seen := make(map[int]struct{}, len(abilities))
	for _, ability := range abilities {
		if _, ok := seen[ability.ChannelId]; ok {
			continue
		}
		seen[ability.ChannelId] = struct{}{}
		channelIds = append(channelIds, ability.ChannelId)
	}

	var channels []*Channel
	if err := DB.Where("id IN ?", channelIds).Find(&channels).Error; err != nil {
		// A lookup failure cannot prove the constraint. With any filter set,
		// drop the candidates instead of routing to a channel that may not match.
		if len(filters) > 0 {
			return nil
		}
		return abilities
	}

	channelsByID := make(map[int]*Channel, len(channels))
	for _, channel := range channels {
		channelsByID[channel.Id] = channel
	}

	filtered := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		channel := channelsByID[ability.ChannelId]
		if ok, _ := ChannelSatisfiesFilters(channel, modelName, filters); ok {
			filtered = append(filtered, ability)
		}
	}
	return filtered
}

func identityFilterRequiresKey(filters []dto.ChannelFilter) bool {
	for _, filter := range filters {
		if filter.Kind == dto.FilterTaskPluginIdentity && filter.TaskPluginKey != "" {
			return true
		}
	}
	return false
}

func (channel *Channel) AddAbilities(tx *gorm.DB) error {
	models_ := common.SplitCommaSeparated(channel.Models)
	groups_ := common.SplitCommaSeparated(channel.Group)
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_)*len(groups_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}
	if len(abilities) == 0 {
		return nil
	}
	// choose DB or provided tx
	useDB := DB
	if tx != nil {
		useDB = tx
	}
	for _, chunk := range lo.Chunk(abilities, 50) {
		err := useDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (channel *Channel) DeleteAbilities() error {
	return DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities(tx *gorm.DB) error {
	isNewTx := false
	// 如果没有传入事务，创建新的事务
	if tx == nil {
		tx = DB.Begin()
		if tx.Error != nil {
			return tx.Error
		}
		isNewTx = true
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()
	}

	// The caller can still hold the status and model list from before a
	// concurrent disable committed. Rebuild from the locked row.
	var stored Channel
	err := lockForUpdate(tx).Select("id", "status", "models", "group", "priority", "weight", "tag").First(&stored, "id = ?", channel.Id).Error
	if err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}
	channel.Status = stored.Status
	channel.Models = stored.Models
	channel.Group = stored.Group
	channel.Priority = stored.Priority
	channel.Weight = stored.Weight
	channel.Tag = stored.Tag

	// First delete all abilities of this channel
	err = tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	if err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}

	// Then add new abilities
	models_ := common.SplitCommaSeparated(channel.Models)
	groups_ := common.SplitCommaSeparated(channel.Group)
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_)*len(groups_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}

	if len(abilities) > 0 {
		for _, chunk := range lo.Chunk(abilities, 50) {
			err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
			if err != nil {
				if isNewTx {
					tx.Rollback()
				}
				return err
			}
		}
	}

	// 如果是新创建的事务，需要提交
	if isNewTx {
		return tx.Commit().Error
	}

	return nil
}

func UpdateAbilityStatus(channelId int, status bool) error {
	return updateAbilityEnabled(DB, channelId, status)
}

func updateAbilityEnabled(tx *gorm.DB, channelId int, enabled bool) error {
	if tx == nil {
		tx = DB
	}
	return tx.Model(&Ability{}).Where("channel_id = ?", channelId).Update("enabled", enabled).Error
}

func UpdateAbilityStatusByTag(tag string, status bool) error {
	return updateAbilityStatusByTag(DB, tag, status)
}

func updateAbilityStatusByTag(tx *gorm.DB, tag string, status bool) error {
	if tx == nil {
		tx = DB
	}
	return tx.Model(&Ability{}).Where("tag = ?", tag).Update("enabled", status).Error
}

func UpdateAbilityByTag(tag string, newTag *string, priority *int64, weight *uint) error {
	return updateAbilityByTag(DB, tag, newTag, priority, weight)
}

func updateAbilityByTag(db *gorm.DB, tag string, newTag *string, priority *int64, weight *uint) error {
	return updateAbilityByTagIDs(db, nil, tag, newTag, priority, weight)
}

func updateAbilityByTagIDs(db *gorm.DB, channelIDs []int, tag string, newTag *string, priority *int64, weight *uint) error {
	if db == nil {
		db = DB
	}
	// A map writes explicit zeros. Struct Updates skips a uint weight of 0, so a
	// tag edit that turns weight off would leave the old ability weight in place.
	updates := map[string]any{}
	if newTag != nil {
		updates["tag"] = *newTag
	}
	if priority != nil {
		updates["priority"] = *priority
	}
	if weight != nil {
		updates["weight"] = *weight
	}
	if len(updates) == 0 {
		return nil
	}
	if channelIDs != nil && len(channelIDs) == 0 {
		return nil
	}
	query := db.Model(&Ability{}).Where("tag = ?", tag)
	if channelIDs != nil {
		query = query.Where("channel_id IN ?", channelIDs)
	}
	return query.Updates(updates).Error
}

var fixLock = sync.Mutex{}

func FixAbility() (int, int, error) {
	lock := fixLock.TryLock()
	if !lock {
		return 0, 0, common.Localized(i18n.MsgAbilityRepairRunning)
	}
	defer fixLock.Unlock()

	// Delete and rebuild in one transaction. A truncate outside the transaction
	// leaves routing with an empty ability table until every channel is rewritten,
	// and a crash in between stays empty. Readers see the old rows until commit.
	var channels []*Channel
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM abilities").Error; err != nil {
			return err
		}
		if err := tx.Find(&channels).Error; err != nil {
			return err
		}
		for _, channel := range channels {
			if err := channel.AddAbilities(tx); err != nil {
				return fmt.Errorf("add abilities for channel %d: %w", channel.Id, err)
			}
		}
		return nil
	})
	if err != nil {
		common.SysLog(fmt.Sprintf("Fix abilities failed: %s", err.Error()))
		return 0, len(channels), err
	}
	InitChannelCache()
	return len(channels), 0, nil
}
