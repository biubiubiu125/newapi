package model

import "strings"

// GetMissingModels returns model names that are referenced in the system
func GetMissingModels() ([]string, error) {
	// 1. 获取所有已启用模型（去重）
	models, err := GetEnabledModelsWithError()
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return []string{}, nil
	}

	// 2. 查询已有的元数据。规则模型也能覆盖多个渠道模型。
	var existing []Model
	if err := DB.Model(&Model{}).Select("model_name", "name_rule").Find(&existing).Error; err != nil {
		return nil, err
	}

	// 3. 收集缺失模型
	var missing []string
	for _, name := range models {
		if !isModelCoveredByMeta(name, existing) {
			missing = append(missing, name)
		}
	}
	return missing, nil
}

func isModelCoveredByMeta(modelName string, metas []Model) bool {
	for _, meta := range metas {
		metaModelName := strings.TrimSpace(meta.ModelName)
		if metaModelName == "" {
			continue
		}
		switch meta.NameRule {
		case NameRulePrefix:
			if strings.HasPrefix(modelName, metaModelName) {
				return true
			}
		case NameRuleSuffix:
			if strings.HasSuffix(modelName, metaModelName) {
				return true
			}
		case NameRuleContains:
			if strings.Contains(modelName, metaModelName) {
				return true
			}
		default:
			if modelName == metaModelName {
				return true
			}
		}
	}
	return false
}
