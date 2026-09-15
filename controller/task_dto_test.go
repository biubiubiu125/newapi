package controller

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestTasksToDtoOmitsInternalFieldsForNonAdmin(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_user_dto",
		Platform:         constant.TaskPlatform("kling"),
		UserId:           9,
		Group:            "vip",
		ChannelId:        7,
		Quota:            100,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		Data: json.RawMessage(`{"bytesBase64Encoded":"AAAA"}`),
		Properties: model.Properties{
			Input:             "a cat",
			OriginModelName:   "kling-v1",
			UpstreamModelName: "secret-upstream",
		},
		PrivateData: model.TaskPrivateData{
			ResultURL:              "https://cdn.example/a.mp4",
			SettlementError:        "record consume log failed",
			SettlementAttemptQuota: 50,
		},
	}

	items := tasksToDto([]*model.Task{task}, false, common.RoleCommonUser)
	require.Len(t, items, 1)
	require.Equal(t, "task_user_dto", items[0].TaskID)
	require.Empty(t, items[0].ResultURL)
	require.Zero(t, items[0].UserId)
	require.Empty(t, items[0].Group)
	require.Zero(t, items[0].ChannelId)
	require.Zero(t, items[0].Quota)
	require.Empty(t, items[0].SettlementStatus)
	require.Empty(t, items[0].SettlementError)
	require.Zero(t, items[0].SettlementAttemptQuota)
	require.Nil(t, items[0].Data)
	props, ok := items[0].Properties.(model.Properties)
	require.True(t, ok)
	require.Equal(t, "a cat", props.Input)
	require.Equal(t, "kling-v1", props.OriginModelName)
	require.Empty(t, props.UpstreamModelName)
}

func TestTasksToDtoKeepsInternalFieldsForAdmin(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_admin_dto",
		UserId:           9,
		Group:            "vip",
		ChannelId:        7,
		Quota:            100,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		Data:             json.RawMessage(`{"ok":true}`),
		PrivateData: model.TaskPrivateData{
			ResultURL:              "https://cdn.example/a.mp4",
			SettlementError:        "record consume log failed",
			SettlementAttemptQuota: 50,
		},
	}

	items := tasksToDto([]*model.Task{task}, false, common.RoleAdminUser)
	require.Len(t, items, 1)
	require.Equal(t, 9, items[0].UserId)
	require.Equal(t, "vip", items[0].Group)
	require.Equal(t, 7, items[0].ChannelId)
	require.Equal(t, 100, items[0].Quota)
	require.Equal(t, model.TaskSettlementStatusReview, items[0].SettlementStatus)
	require.Equal(t, "record consume log failed", items[0].SettlementError)
	require.Equal(t, 50, items[0].SettlementAttemptQuota)
	require.JSONEq(t, `{"ok":true}`, string(items[0].Data))
}
