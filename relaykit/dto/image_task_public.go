package dto

type PublicImageTask struct {
	TaskID               string                `json:"task_id"`
	ClientTaskID         string                `json:"client_task_id,omitempty"`
	Status               string                `json:"status"`
	Progress             string                `json:"progress,omitempty"`
	CreatedAt            int64                 `json:"created_at"`
	UpdatedAt            int64                 `json:"updated_at"`
	StartedAt            int64                 `json:"started_at,omitempty"`
	CompletedAt          int64                 `json:"completed_at,omitempty"`
	ResultAvailable      bool                  `json:"result_available"`
	ResultExpiresAt      int64                 `json:"result_expires_at,omitempty"`
	ResultAcknowledgedAt int64                 `json:"result_acknowledged_at,omitempty"`
	Error                *PublicImageTaskError `json:"error,omitempty"`
}

type PublicImageTaskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PublicImageTaskList struct {
	Data []*PublicImageTask `json:"data"`
	// NotFoundIDs lists requested task IDs that are not visible to the current
	// user+token (missing, other token, or non-public). It does not reveal why.
	NotFoundIDs []string `json:"not_found_ids,omitempty"`
}
