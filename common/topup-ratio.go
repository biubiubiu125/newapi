package common

import (
	"encoding/json"
	"strings"
	"sync"
)

var topupGroupRatio = map[string]float64{
	"default": 1,
	"vip":     1,
	"svip":    1,
}
var topupGroupRatioMutex sync.RWMutex

func TopupGroupRatio2JSONString() string {
	topupGroupRatioMutex.RLock()
	defer topupGroupRatioMutex.RUnlock()
	jsonBytes, err := json.Marshal(topupGroupRatio)
	if err != nil {
		SysError("error marshalling topup group ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateTopupGroupRatioByJSONString(jsonStr string) error {
	next := make(map[string]float64)
	if err := json.Unmarshal([]byte(jsonStr), &next); err != nil {
		return err
	}
	cleaned := make(map[string]float64, len(next))
	for name, ratio := range next {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		cleaned[name] = ratio
	}

	topupGroupRatioMutex.Lock()
	defer topupGroupRatioMutex.Unlock()
	topupGroupRatio = cleaned
	return nil
}

func GetTopupGroupRatio(name string) float64 {
	name = strings.TrimSpace(name)
	topupGroupRatioMutex.RLock()
	defer topupGroupRatioMutex.RUnlock()
	ratio, ok := topupGroupRatio[name]
	if !ok {
		SysError("topup group ratio not found: " + name)
		return 1
	}
	return ratio
}
