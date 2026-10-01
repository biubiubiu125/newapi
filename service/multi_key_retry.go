package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const contextKeyMultiKeyRetryHold = "multi_key_retry_hold"

// MaxMultiKeyFailuresBeforeFailover is how many keys on one channel a single
// request may try before that channel is excluded. These attempts do not spend
// RetryTimes. The cross-channel budget starts only after the channel is out.
const MaxMultiKeyFailuresBeforeFailover = 3

type multiKeyRetryHold struct {
	ChannelID int
	UsedKeys  map[string]struct{}
	Attempts  int
}

func ClearMultiKeyRetryHold(c *gin.Context) {
	if c != nil {
		c.Set(contextKeyMultiKeyRetryHold, nil)
	}
}

func multiKeyRetryHoldFrom(c *gin.Context) *multiKeyRetryHold {
	if c == nil {
		return nil
	}
	raw, ok := c.Get(contextKeyMultiKeyRetryHold)
	if !ok || raw == nil {
		return nil
	}
	hold, _ := raw.(*multiKeyRetryHold)
	return hold
}

// snapshotMultiKey copies the key list and status map under the same polling
// lock that UpdateChannelStatus uses to mutate MultiKeyStatusList. Reading the
// live map without that lock panics under concurrent auto-disable.
func snapshotMultiKey(channel *model.Channel) (keys []string, status map[int]int, enabled bool, multi bool) {
	if channel == nil {
		return nil, nil, false, false
	}
	lock := model.GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()
	keys = append([]string(nil), channel.GetKeys()...)
	if src := channel.ChannelInfo.MultiKeyStatusList; len(src) > 0 {
		status = make(map[int]int, len(src))
		for index, keyStatus := range src {
			status[index] = keyStatus
		}
	}
	return keys, status, channel.Status == common.ChannelStatusEnabled, channel.ChannelInfo.IsMultiKey
}

func NextUnusedEnabledKey(channel *model.Channel, used map[string]struct{}) (string, bool) {
	fresh, ok := freshMultiKeyChannel(channel)
	if !ok {
		return "", false
	}
	keys, status, _, _ := snapshotMultiKey(fresh)
	for i, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, taken := used[key]; taken {
			continue
		}
		if status != nil {
			if keyStatus, exists := status[i]; exists && keyStatus != common.ChannelStatusEnabled {
				continue
			}
		}
		return key, true
	}
	return "", false
}

// RememberFailedMultiKey keeps the next retry on this channel when another
// enabled key has not been used. The distributor stub does not carry the key
// list, so the real channel is loaded here.
func RememberFailedMultiKey(c *gin.Context, channelID int, failedKey string) bool {
	if c == nil || channelID <= 0 || !common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
		ClearMultiKeyRetryHold(c)
		return false
	}
	channel, err := model.GetChannelById(channelID, true)
	if err != nil || channel == nil {
		ClearMultiKeyRetryHold(c)
		return false
	}
	_, _, enabled, multi := snapshotMultiKey(channel)
	if !multi || !enabled {
		ClearMultiKeyRetryHold(c)
		return false
	}
	hold := multiKeyRetryHoldFrom(c)
	if hold == nil || hold.ChannelID != channelID {
		hold = &multiKeyRetryHold{ChannelID: channelID, UsedKeys: map[string]struct{}{}}
	}
	if hold.UsedKeys == nil {
		hold.UsedKeys = map[string]struct{}{}
	}
	if failedKey = strings.TrimSpace(failedKey); failedKey != "" {
		hold.UsedKeys[failedKey] = struct{}{}
	}
	hold.Attempts++
	// Stop on this channel once its own key budget is spent, even if more keys
	// remain. Otherwise one long key list consumes the request before any peer.
	if hold.Attempts >= MaxMultiKeyFailuresBeforeFailover {
		ClearMultiKeyRetryHold(c)
		return false
	}
	if _, ok := NextUnusedEnabledKey(channel, hold.UsedKeys); !ok {
		ClearMultiKeyRetryHold(c)
		return false
	}
	c.Set(contextKeyMultiKeyRetryHold, hold)
	return true
}

func TakeMultiKeyRetryKey(c *gin.Context) (*model.Channel, string, bool) {
	hold := multiKeyRetryHoldFrom(c)
	if hold == nil || hold.ChannelID <= 0 {
		return nil, "", false
	}
	channel, err := model.GetChannelById(hold.ChannelID, true)
	if err != nil || channel == nil {
		ClearMultiKeyRetryHold(c)
		return nil, "", false
	}
	_, _, enabled, multi := snapshotMultiKey(channel)
	if !enabled || !multi {
		ClearMultiKeyRetryHold(c)
		return nil, "", false
	}
	key, ok := NextUnusedEnabledKey(channel, hold.UsedKeys)
	if !ok {
		ClearMultiKeyRetryHold(c)
		return nil, "", false
	}
	return channel, key, true
}

func freshMultiKeyChannel(channel *model.Channel) (*model.Channel, bool) {
	if channel == nil {
		return nil, false
	}
	if channel.Id <= 0 {
		return channel, true
	}
	fresh, err := model.GetChannelById(channel.Id, true)
	if err != nil || fresh == nil {
		return nil, false
	}
	return fresh, true
}

func FailedChannelIDs(c *gin.Context) []int {
	if c == nil {
		return nil
	}
	raw, exists := c.Get("failed_channel_ids")
	if !exists || raw == nil {
		return nil
	}
	ids, ok := raw.([]int)
	if !ok {
		return nil
	}
	return ids
}

func AddFailedChannelID(c *gin.Context, channelID int) {
	if c == nil || channelID <= 0 {
		return
	}
	failed := FailedChannelIDs(c)
	for _, id := range failed {
		if id == channelID {
			return
		}
	}
	copied := append([]int{}, failed...)
	c.Set("failed_channel_ids", append(copied, channelID))
}
