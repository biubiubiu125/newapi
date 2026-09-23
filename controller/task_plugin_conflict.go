package controller

import (
	"regexp"
	"strconv"

	"github.com/QuantumNous/new-api/i18n"
)

var (
	pluginModelConflict    = regexp.MustCompile(`^plugin (\S+) model ("(?:\\.|[^"\\])*") conflicts with plugin (\S+) model ("(?:\\.|[^"\\])*")$`)
	pluginChannelConflict  = regexp.MustCompile(`^plugin (\S+) channelType (\d+) conflicts with plugin (\S+)$`)
	pluginRouteConflict    = regexp.MustCompile(`^plugin (\S+) route (\S+) (\S+) conflicts with plugin (\S+) route (\S+)$`)
	pluginProtocolConflict = regexp.MustCompile(`^plugin (\S+) protocol (\S+) (\S+) model ("(?:\\.|[^"\\])*") conflicts with plugin (\S+)$`)
)

func taskPluginConflictMessage(err error) (string, map[string]any, bool) {
	if err == nil {
		return "", nil, false
	}
	message := err.Error()
	if match := pluginModelConflict.FindStringSubmatch(message); match != nil {
		model, errModel := strconv.Unquote(match[2])
		otherModel, errOther := strconv.Unquote(match[4])
		if errModel != nil || errOther != nil {
			return "", nil, false
		}
		return i18n.MsgPluginModelConflict, map[string]any{
			"Plugin":     match[1],
			"Model":      strconv.Quote(model),
			"Other":      match[3],
			"OtherModel": strconv.Quote(otherModel),
		}, true
	}
	if match := pluginChannelConflict.FindStringSubmatch(message); match != nil {
		return i18n.MsgPluginChannelTypeConflict, map[string]any{
			"Plugin":      match[1],
			"ChannelType": match[2],
			"Other":       match[3],
		}, true
	}
	if match := pluginRouteConflict.FindStringSubmatch(message); match != nil {
		return i18n.MsgPluginRouteConflict, map[string]any{
			"Plugin":    match[1],
			"Method":    match[2],
			"Path":      match[3],
			"Other":     match[4],
			"OtherPath": match[5],
		}, true
	}
	if match := pluginProtocolConflict.FindStringSubmatch(message); match != nil {
		model, errModel := strconv.Unquote(match[4])
		if errModel != nil {
			return "", nil, false
		}
		return i18n.MsgPluginProtocolConflict, map[string]any{
			"Plugin": match[1],
			"Method": match[2],
			"Path":   match[3],
			"Model":  strconv.Quote(model),
			"Other":  match[5],
		}, true
	}
	return "", nil, false
}
