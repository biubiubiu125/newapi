package dto

import "encoding/json"

const (
	ImageTaskStatusQueued  = "queued"
	ImageTaskStatusRunning = "running"
	ImageTaskStatusSuccess = "success"
	ImageTaskStatusFailed  = "failed"

	ImageTaskDefaultGenerationModel = "dall-e"
)

type ImageTaskCreateResponse struct {
	TaskID       string `json:"task_id"`
	ClientTaskID string `json:"client_task_id,omitempty"`
	Status       string `json:"status"`
	CreatedAt    int64  `json:"created_at"`
}

type ImageTaskResponse struct {
	TaskID       string          `json:"task_id"`
	ClientTaskID string          `json:"client_task_id,omitempty"`
	Status       string          `json:"status"`
	Progress     string          `json:"progress,omitempty"`
	CreatedAt    int64           `json:"created_at"`
	UpdatedAt    int64           `json:"updated_at"`
	Error        string          `json:"error,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
}

type ImageTaskGPTImage2APIItem struct {
	ID           string          `json:"id"`
	TaskID       string          `json:"task_id"`
	ClientTaskID string          `json:"client_task_id,omitempty"`
	Status       string          `json:"status"`
	Progress     string          `json:"progress,omitempty"`
	CreatedAt    int64           `json:"created_at"`
	UpdatedAt    int64           `json:"updated_at"`
	Error        string          `json:"error,omitempty"`
	Data         json.RawMessage `json:"data,omitempty"`
	Usage        json.RawMessage `json:"usage,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
}
