package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

type SystemTaskStatus string

const (
	SystemTaskStatusPending   SystemTaskStatus = "pending"
	SystemTaskStatusRunning   SystemTaskStatus = "running"
	SystemTaskStatusSucceeded SystemTaskStatus = "succeeded"
	SystemTaskStatusFailed    SystemTaskStatus = "failed"
	SystemTaskStatusCancelled SystemTaskStatus = "cancelled"

	SystemTaskTypeLogCleanup          = "log_cleanup"
	SystemTaskTypeChannelTest         = "channel_test"
	SystemTaskTypeModelUpdate         = "model_update"
	SystemTaskTypeModelUpdateManual   = "model_update_manual"
	SystemTaskTypeModelUpdateApplyAll = "model_update_apply_all"
	SystemTaskTypeMidjourneyPoll      = "midjourney_poll"
	SystemTaskTypeAsyncTaskPoll       = "async_task_poll"
)

var ErrSystemTaskLockLost = errors.New("system task lock lost")

type SystemTask struct {
	ID        int64            `json:"id" gorm:"primary_key"`
	TaskID    string           `json:"task_id" gorm:"type:varchar(64);uniqueIndex"`
	Type      string           `json:"type" gorm:"type:varchar(64);index"`
	Status    SystemTaskStatus `json:"status" gorm:"type:varchar(32);index"`
	ActiveKey *string          `json:"active_key,omitempty" gorm:"type:varchar(64);uniqueIndex"`
	Payload   string           `json:"payload" gorm:"type:text"`
	State     string           `json:"state" gorm:"type:text"`
	Result    string           `json:"result" gorm:"type:text"`
	Error     string           `json:"error" gorm:"type:text"`
	LockedBy  string           `json:"locked_by" gorm:"type:varchar(128);index"`
	CreatedAt int64            `json:"created_at" gorm:"bigint;index"`
	UpdatedAt int64            `json:"updated_at" gorm:"bigint;index"`
}

type SystemTaskLock struct {
	Type        string `json:"type" gorm:"type:varchar(64);primaryKey"`
	TaskID      string `json:"task_id" gorm:"type:varchar(64);index"`
	LockedBy    string `json:"locked_by" gorm:"type:varchar(128);index"`
	LockedUntil int64  `json:"locked_until" gorm:"bigint;index"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint;index"`
}

type SystemTaskResponse struct {
	ID        int64            `json:"id"`
	TaskID    string           `json:"task_id"`
	Type      string           `json:"type"`
	Status    SystemTaskStatus `json:"status"`
	ActiveKey *string          `json:"active_key,omitempty"`
	Payload   any              `json:"payload"`
	State     any              `json:"state"`
	Result    any              `json:"result"`
	Error     string           `json:"error"`
	LockedBy  string           `json:"locked_by"`
	CreatedAt int64            `json:"created_at"`
	UpdatedAt int64            `json:"updated_at"`
}

func (task *SystemTask) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if task.CreatedAt == 0 {
		task.CreatedAt = now
	}
	if task.UpdatedAt == 0 {
		task.UpdatedAt = now
	}
	return nil
}

func (lock *SystemTaskLock) BeforeCreate(_ *gorm.DB) error {
	if lock.UpdatedAt == 0 {
		lock.UpdatedAt = common.GetTimestamp()
	}
	return nil
}

func GenerateSystemTaskID() (string, error) {
	key, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return "", err
	}
	return "systask_" + key, nil
}

func CreateSystemTask(taskType string, payload any, state any) (*SystemTask, error) {
	return CreateSystemTaskWithActiveKey(taskType, payload, state, taskType)
}

func CreateSystemTaskWithActiveKey(taskType string, payload any, state any, activeKey string) (*SystemTask, error) {
	taskID, err := GenerateSystemTaskID()
	if err != nil {
		return nil, err
	}
	payloadText, err := marshalSystemTaskJSON(payload)
	if err != nil {
		return nil, err
	}
	stateText, err := marshalSystemTaskJSON(state)
	if err != nil {
		return nil, err
	}

	activeKey = strings.TrimSpace(activeKey)
	if activeKey == "" {
		activeKey = taskType
	}
	if err := clearFinishedSystemTaskActiveKey(activeKey); err != nil {
		return nil, err
	}
	task := &SystemTask{
		TaskID:    taskID,
		Type:      taskType,
		Status:    SystemTaskStatusPending,
		ActiveKey: &activeKey,
		Payload:   payloadText,
		State:     stateText,
	}

	if err := DB.Create(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}

func clearFinishedSystemTaskActiveKey(activeKey string) error {
	activeKey = strings.TrimSpace(activeKey)
	if activeKey == "" {
		return nil
	}
	return DB.Model(&SystemTask{}).
		Where("active_key = ? AND status NOT IN ?", activeKey, activeSystemTaskStatuses()).
		Update("active_key", nil).Error
}

func GetSystemTaskByTaskID(taskID string) (*SystemTask, error) {
	var task SystemTask
	if err := DB.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func GetActiveSystemTask(taskType string) (*SystemTask, error) {
	var task SystemTask
	err := DB.Where("type = ? AND status IN ?", taskType, activeSystemTaskStatuses()).
		Order("id desc").
		First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func GetActiveSystemTaskByActiveKey(activeKey string) (*SystemTask, error) {
	tasks, err := GetActiveSystemTasksByActiveKeys([]string{activeKey})
	if err != nil || len(tasks) == 0 {
		return nil, err
	}
	return tasks[0], nil
}

func GetActiveSystemTasksByActiveKeys(activeKeys []string) ([]*SystemTask, error) {
	normalizedKeys := normalizeSystemTaskLookupValues(activeKeys)
	if len(normalizedKeys) == 0 {
		return nil, nil
	}
	var tasks []*SystemTask
	err := DB.Where("active_key IN ? AND status IN ?", normalizedKeys, activeSystemTaskStatuses()).
		Order("id desc").
		Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

// GetActiveSystemTasksByActiveKeysOrLegacyTypes returns active tasks identified
// by the current active-key protocol or by a legacy row created before
// active_key was introduced. Legacy matching is intentionally restricted to
// rows with a NULL or empty active_key so a stale type-only row cannot bypass a
// newer lock type. The empty-string branch covers rows created by early
// migrations or drivers that stored the zero value instead of SQL NULL.
func GetActiveSystemTasksByActiveKeysOrLegacyTypes(activeKeys []string, legacyTaskTypes []string) ([]*SystemTask, error) {
	normalizedKeys := normalizeSystemTaskLookupValues(activeKeys)
	normalizedTypes := normalizeSystemTaskLookupValues(legacyTaskTypes)
	if len(normalizedKeys) == 0 && len(normalizedTypes) == 0 {
		return nil, nil
	}

	if len(normalizedKeys) > 0 {
		activeTasks, err := queryActiveSystemTasksByActiveKeys(normalizedKeys)
		if err != nil {
			return nil, err
		}
		if len(activeTasks) > 0 {
			return activeTasks, nil
		}

		lockTasks, err := querySystemTasksByActiveLocks(normalizedKeys, normalizedTypes)
		if err != nil {
			return nil, err
		}
		if len(lockTasks) > 0 || len(normalizedTypes) == 0 {
			return lockTasks, nil
		}
	}

	return queryLegacyActiveSystemTasksByTypes(normalizedTypes)
}

func queryActiveSystemTasksByActiveKeys(activeKeys []string) ([]*SystemTask, error) {
	var tasks []*SystemTask
	if err := DB.Where("status IN ?", activeSystemTaskStatuses()).
		Where("active_key IN ?", activeKeys).
		Order("id desc").
		Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func querySystemTasksByActiveLocks(activeKeys []string, taskTypes []string) ([]*SystemTask, error) {
	normalizedKeys := normalizeSystemTaskLookupValues(activeKeys)
	normalizedTypes := normalizeSystemTaskLookupValues(taskTypes)
	if len(normalizedKeys) == 0 {
		return nil, nil
	}

	now := common.GetTimestamp()
	query := DB.
		Table("system_tasks").
		Select("system_tasks.*").
		Joins("JOIN system_task_locks ON system_task_locks.task_id = system_tasks.task_id").
		Where("system_tasks.status IN ?", activeSystemTaskStatuses()).
		Where("system_task_locks.locked_by = system_tasks.locked_by").
		Where("system_task_locks.type IN ? AND system_task_locks.locked_until >= ?", normalizedKeys, now)
	if len(normalizedTypes) > 0 {
		query = query.Where("system_tasks.type IN ?", normalizedTypes)
	}

	var tasks []*SystemTask
	if err := query.Order("system_tasks.id desc").Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func queryLegacyActiveSystemTasksByTypes(legacyTaskTypes []string) ([]*SystemTask, error) {
	normalizedTypes := normalizeSystemTaskLookupValues(legacyTaskTypes)
	if len(normalizedTypes) == 0 {
		return nil, nil
	}

	var tasks []*SystemTask
	if err := DB.Where("status IN ?", activeSystemTaskStatuses()).
		Where("(active_key IS NULL OR active_key = '') AND type IN ?", normalizedTypes).
		Order("id desc").
		Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func FindPendingSystemTasks(taskType string, limit int) ([]*SystemTask, error) {
	var tasks []*SystemTask
	if limit <= 0 {
		limit = 1
	}
	err := DB.Where("type = ? AND status = ?", taskType, SystemTaskStatusPending).
		Order("id asc").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func FindEarliestPendingSystemTasks(taskTypes []string) (map[string]*SystemTask, error) {
	tasksByType := map[string]*SystemTask{}
	if len(taskTypes) == 0 {
		return tasksByType, nil
	}

	subQuery := DB.Model(&SystemTask{}).
		Select("MIN(id)").
		Where("type IN ? AND status = ?", taskTypes, SystemTaskStatusPending).
		Group("type")
	var tasks []*SystemTask
	if err := DB.Where("id IN (?)", subQuery).Find(&tasks).Error; err != nil {
		return nil, err
	}
	for _, task := range tasks {
		tasksByType[task.Type] = task
	}
	return tasksByType, nil
}

func ListSystemTasks(limit int) ([]*SystemTask, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var tasks []*SystemTask
	err := DB.Order("id desc").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// GetLatestSystemTask returns the most recent task row of the given type
// (any status) so the scheduler can decide whether enough time has elapsed
// since the last run. Returns (nil, nil) when no row exists.
func GetLatestSystemTask(taskType string) (*SystemTask, error) {
	var task SystemTask
	err := DB.Where("type = ?", taskType).Order("id desc").First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func GetLatestSystemTasks(taskTypes []string) (map[string]*SystemTask, error) {
	tasksByType := map[string]*SystemTask{}
	if len(taskTypes) == 0 {
		return tasksByType, nil
	}

	subQuery := DB.Model(&SystemTask{}).
		Select("MAX(id)").
		Where("type IN ?", taskTypes).
		Group("type")
	var tasks []*SystemTask
	if err := DB.Where("id IN (?)", subQuery).Find(&tasks).Error; err != nil {
		return nil, err
	}
	for _, task := range tasks {
		tasksByType[task.Type] = task
	}
	return tasksByType, nil
}

func ClaimSystemTask(id int64, taskType string, lockType string, runnerID string, lockUntil int64) (*SystemTask, bool, error) {
	now := common.GetTimestamp()
	var task SystemTask
	if err := DB.Where("id = ? AND type = ? AND status = ?", id, taskType, SystemTaskStatusPending).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}

	if lockType == "" {
		lockType = taskType
	}

	acquired, err := acquireSystemTaskLock(lockType, task.TaskID, runnerID, now, lockUntil)
	if err != nil || !acquired {
		return nil, acquired, err
	}

	result := DB.Model(&SystemTask{}).
		Where("id = ? AND type = ? AND status = ?", id, taskType, SystemTaskStatusPending).
		Where(
			"EXISTS (SELECT 1 FROM system_task_locks WHERE system_task_locks.type = ? AND system_task_locks.task_id = ? AND system_task_locks.locked_by = ? AND system_task_locks.locked_until >= ?)",
			lockType,
			task.TaskID,
			runnerID,
			now,
		).
		Updates(map[string]any{
			"status":     SystemTaskStatusRunning,
			"locked_by":  runnerID,
			"updated_at": now,
		})
	if result.Error != nil {
		_ = ReleaseSystemTaskLock(task.TaskID, runnerID)
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		_ = ReleaseSystemTaskLock(task.TaskID, runnerID)
		return nil, false, nil
	}

	if err := DB.Where("id = ?", id).First(&task).Error; err != nil {
		_ = ReleaseSystemTaskLock(task.TaskID, runnerID)
		_ = MarkSystemTaskFailedForRunner(task.TaskID, runnerID, err.Error())
		return nil, false, err
	}
	return &task, true, nil
}

func acquireSystemTaskLock(taskType string, taskID string, lockedBy string, now int64, lockUntil int64) (bool, error) {
	lock := &SystemTaskLock{
		Type:        taskType,
		TaskID:      taskID,
		LockedBy:    lockedBy,
		LockedUntil: lockUntil,
		UpdatedAt:   now,
	}
	createErr := DB.Create(lock).Error
	if createErr == nil {
		return true, nil
	}

	var existing SystemTaskLock
	err := DB.Where("type = ?", taskType).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// A failed insert with no existing row is not lock contention. Do
			// not turn schema, connection, trigger, or other database errors
			// into a silent pending task that can never be claimed.
			return false, createErr
		}
		return false, err
	}

	acquired := false
	err = DB.Transaction(func(tx *gorm.DB) error {
		var current SystemTaskLock
		if err := tx.Where("type = ?", taskType).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return createErr
			}
			return err
		}

		var existingTask SystemTask
		taskErr := LockForUpdate(tx).
			Where("task_id = ?", current.TaskID).
			First(&existingTask).Error
		if taskErr != nil && !errors.Is(taskErr, gorm.ErrRecordNotFound) {
			return taskErr
		}
		if current.LockedUntil >= now &&
			taskErr == nil &&
			(existingTask.Status == SystemTaskStatusRunning ||
				(existingTask.Status == SystemTaskStatusPending &&
					strings.TrimSpace(existingTask.LockedBy) != "")) {
			return nil
		}

		if current.LockedUntil < now &&
			taskErr == nil &&
			existingTask.Status == SystemTaskStatusRunning {
			// Expire the old run in the same transaction that replaces its
			// lock. Otherwise a successful claim could leave the old running
			// row active if the follow-up status update fails.
			if err := markSystemTaskLeaseExpiredWithTx(tx, existingTask.TaskID); err != nil {
				return err
			}
		}

		var lockedCurrent SystemTaskLock
		if err := LockForUpdate(tx).
			Where(
				"type = ? AND task_id = ? AND locked_by = ? AND locked_until = ?",
				current.Type,
				current.TaskID,
				current.LockedBy,
				current.LockedUntil,
			).
			First(&lockedCurrent).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		result := tx.Model(&SystemTaskLock{}).
			Where(
				"type = ? AND task_id = ? AND locked_by = ? AND locked_until = ?",
				lockedCurrent.Type,
				lockedCurrent.TaskID,
				lockedCurrent.LockedBy,
				lockedCurrent.LockedUntil,
			).
			Updates(map[string]any{
				"task_id":      taskID,
				"locked_by":    lockedBy,
				"locked_until": lockUntil,
				"updated_at":   now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		acquired = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if !acquired {
		return false, nil
	}
	return true, nil
}

// CleanupInactiveSystemTaskLocks removes live locks that can no longer belong
// to an executable task. A terminal or missing task cannot be running, so
// retaining its lock only blocks the next task until the lease TTL expires.
func CleanupInactiveSystemTaskLocks(lockType string, now int64) error {
	lockType = strings.TrimSpace(lockType)
	if lockType == "" {
		return errors.New("lock type is required")
	}
	if now <= 0 {
		now = common.GetTimestamp()
	}

	var locks []*SystemTaskLock
	if err := DB.
		Where("type = ? AND locked_until >= ?", lockType, now).
		Find(&locks).Error; err != nil {
		return err
	}
	for _, candidate := range locks {
		if candidate == nil {
			continue
		}
		if err := DB.Transaction(func(tx *gorm.DB) error {
			var task SystemTask
			taskErr := LockForUpdate(tx).
				Where("task_id = ?", candidate.TaskID).
				First(&task).Error
			if taskErr != nil && !errors.Is(taskErr, gorm.ErrRecordNotFound) {
				return taskErr
			}
			if taskErr == nil &&
				(task.Status == SystemTaskStatusPending ||
					task.Status == SystemTaskStatusRunning) {
				return nil
			}

			var current SystemTaskLock
			if err := LockForUpdate(tx).
				Where(
					"type = ? AND task_id = ? AND locked_by = ? AND locked_until >= ?",
					candidate.Type,
					candidate.TaskID,
					candidate.LockedBy,
					now,
				).
				First(&current).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			return tx.Delete(&current).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func UpdateSystemTaskState(taskID string, lockedBy string, state any) error {
	stateText, err := marshalSystemTaskJSON(state)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	result := DB.Model(&SystemTask{}).
		Where("task_id = ? AND status = ? AND locked_by = ?", taskID, SystemTaskStatusRunning, lockedBy).
		Where("EXISTS (SELECT 1 FROM system_task_locks WHERE system_task_locks.task_id = system_tasks.task_id AND system_task_locks.locked_by = ? AND system_task_locks.locked_until >= ?)", lockedBy, now).
		Updates(map[string]any{
			"state":      stateText,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSystemTaskLockLost
	}
	return nil
}

func RenewSystemTaskLock(taskID string, lockedBy string, lockUntil int64) error {
	now := common.GetTimestamp()
	result := DB.Model(&SystemTaskLock{}).
		Where("task_id = ? AND locked_by = ? AND locked_until >= ?", taskID, lockedBy, now).
		Where("EXISTS (SELECT 1 FROM system_tasks WHERE system_tasks.task_id = system_task_locks.task_id AND system_tasks.locked_by = ? AND system_tasks.status = ?)", lockedBy, SystemTaskStatusRunning).
		Updates(map[string]any{
			"locked_until": lockUntil,
			"updated_at":   now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSystemTaskLockLost
	}
	return nil
}

// LockSystemTaskForExecution locks a running task row while a handler commits
// related business data. Cancellation uses the same row lock, so it cannot
// change the task status in the middle of this transaction. If cancellation
// follows a committed business transaction but precedes task finalization, the
// task may be reported as cancelled while the already-committed business change
// remains; subsequent writes are fenced by the cancelled task status.
func LockSystemTaskForExecution(tx *gorm.DB, taskID string, lockedBy string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	if tx == nil {
		tx = DB
	}
	query := LockForUpdate(tx).
		Where("task_id = ? AND status = ?", taskID, SystemTaskStatusRunning)
	lockedBy = strings.TrimSpace(lockedBy)
	if lockedBy != "" {
		query = query.Where("locked_by = ?", lockedBy)
	}
	query = query.Where(
		"EXISTS (SELECT 1 FROM system_task_locks WHERE system_task_locks.task_id = system_tasks.task_id AND system_task_locks.locked_until >= ?)",
		common.GetTimestamp(),
	)
	if lockedBy != "" {
		query = query.Where(
			"EXISTS (SELECT 1 FROM system_task_locks WHERE system_task_locks.task_id = system_tasks.task_id AND system_task_locks.locked_by = ? AND system_task_locks.locked_until >= ?)",
			lockedBy,
			common.GetTimestamp(),
		)
	}
	var task SystemTask
	if err := query.First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSystemTaskLockLost
		}
		return err
	}
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		// SQLite skips FOR UPDATE. Turn the validation into a write lock before
		// the handler touches channel data, so cancellation cannot commit
		// between the task check and the business update.
		result := tx.Model(&SystemTask{}).
			Where("id = ? AND status = ? AND locked_by = ?", task.ID, SystemTaskStatusRunning, task.LockedBy).
			UpdateColumn("updated_at", gorm.Expr("updated_at"))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrSystemTaskLockLost
		}
	}
	return nil
}

// MarkSystemTaskFailed records a terminal failure without requiring the
// executor lease. It is used when the runner cannot safely start or renew a
// lease and therefore must not leave a row stuck in running forever.
func MarkSystemTaskFailed(taskID string, errorMessage string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return errors.New("task id is required")
	}
	errorMessage = strings.TrimSpace(errorMessage)
	if errorMessage == "" {
		errorMessage = "system task lease renewal failed"
	}
	return DB.Model(&SystemTask{}).
		Where("task_id = ? AND status = ?", taskID, SystemTaskStatusRunning).
		Updates(map[string]any{
			"status":     SystemTaskStatusFailed,
			"active_key": nil,
			"error":      errorMessage,
			"updated_at": common.GetTimestamp(),
		}).Error
}

// MarkSystemTaskFailedForRunner records a lease-related failure only when the
// task is still owned by the runner that reported it. This prevents a stale
// heartbeat from changing a task after ownership has moved elsewhere.
func MarkSystemTaskFailedForRunner(taskID string, lockedBy string, errorMessage string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		return markSystemTaskFailedForRunnerWithTx(tx, taskID, lockedBy, errorMessage)
	})
}

func markSystemTaskFailedForRunnerWithTx(tx *gorm.DB, taskID string, lockedBy string, errorMessage string) error {
	if tx == nil {
		tx = DB
	}
	taskID = strings.TrimSpace(taskID)
	lockedBy = strings.TrimSpace(lockedBy)
	if taskID == "" {
		return errors.New("task id is required")
	}
	if lockedBy == "" {
		return errors.New("runner id is required")
	}
	errorMessage = strings.TrimSpace(errorMessage)
	if errorMessage == "" {
		errorMessage = "system task lease renewal failed"
	}

	var task SystemTask
	err := LockForUpdate(tx).
		Where("task_id = ? AND status = ? AND locked_by = ?", taskID, SystemTaskStatusRunning, lockedBy).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	result := tx.Model(&SystemTask{}).
		Where("id = ? AND status = ? AND locked_by = ?", task.ID, SystemTaskStatusRunning, lockedBy).
		Updates(map[string]any{
			"status":     SystemTaskStatusFailed,
			"active_key": nil,
			"error":      errorMessage,
			"updated_at": common.GetTimestamp(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	if err := tx.Where("task_id = ? AND locked_by = ?", task.TaskID, lockedBy).
		Delete(&SystemTaskLock{}).Error; err != nil {
		return err
	}
	return nil
}

func CancelSystemTask(taskID string, taskTypes []string, errorMessage string) (*SystemTask, bool, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, false, errors.New("task id is required")
	}
	normalizedTypes := normalizeSystemTaskLookupValues(taskTypes)
	if strings.TrimSpace(errorMessage) == "" {
		errorMessage = "task cancelled"
	}

	var task *SystemTask
	cancelled := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var lockedTask SystemTask
		query := LockForUpdate(tx).Where("task_id = ?", taskID)
		if len(normalizedTypes) > 0 {
			query = query.Where("type IN ?", normalizedTypes)
		}
		if err := query.First(&lockedTask).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if lockedTask.Status != SystemTaskStatusPending && lockedTask.Status != SystemTaskStatusRunning {
			task = &lockedTask
			return tx.Where("task_id = ?", lockedTask.TaskID).
				Delete(&SystemTaskLock{}).Error
		}

		now := common.GetTimestamp()
		result := tx.Model(&SystemTask{}).
			Where("id = ? AND status IN ?", lockedTask.ID, activeSystemTaskStatuses()).
			Updates(map[string]any{
				"status":     SystemTaskStatusCancelled,
				"active_key": nil,
				"error":      errorMessage,
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrSystemTaskLockLost
		}
		if err := tx.
			Where("task_id = ?", lockedTask.TaskID).
			Delete(&SystemTaskLock{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", lockedTask.ID).First(&lockedTask).Error; err != nil {
			return err
		}
		task = &lockedTask
		cancelled = true
		return nil
	})
	return task, cancelled, err
}

func MarkSystemTaskLeaseExpired(taskID string) error {
	return markSystemTaskLeaseExpiredWithTx(DB, taskID)
}

// MarkSystemTaskLeaseExpiredForRunner marks the task failed only while the
// reported runner still owns its task row.
func MarkSystemTaskLeaseExpiredForRunner(taskID string, lockedBy string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		return markSystemTaskLeaseExpiredForRunnerWithTx(tx, taskID, lockedBy)
	})
}

func markSystemTaskLeaseExpiredForRunnerWithTx(tx *gorm.DB, taskID string, lockedBy string) error {
	return markSystemTaskFailedForRunnerWithTx(tx, taskID, lockedBy, "task lease expired")
}

func markSystemTaskLeaseExpiredWithTx(tx *gorm.DB, taskID string) error {
	if tx == nil {
		tx = DB
	}
	result := tx.Model(&SystemTask{}).
		Where("task_id = ? AND status = ?", taskID, SystemTaskStatusRunning).
		Updates(map[string]any{
			"status":     SystemTaskStatusFailed,
			"active_key": nil,
			"error":      "task lease expired",
			"updated_at": common.GetTimestamp(),
		})
	return result.Error
}

func ExpireStaleSystemTaskLocks(now int64) error {
	var staleLocks []*SystemTaskLock
	if err := DB.
		Select("type", "task_id", "locked_by", "locked_until").
		Where("locked_until < ?", now).
		Find(&staleLocks).Error; err != nil {
		return err
	}

	for _, staleLock := range staleLocks {
		if staleLock == nil {
			continue
		}
		if err := DB.Transaction(func(tx *gorm.DB) error {
			// Business transactions lock the task row before touching related
			// data. Use the same order here so expiry cannot race a committed
			// channel update.
			var task SystemTask
			taskErr := LockForUpdate(tx).
				Where("task_id = ?", staleLock.TaskID).
				First(&task).Error
			if errors.Is(taskErr, gorm.ErrRecordNotFound) {
				taskErr = nil
			}
			if taskErr != nil {
				return taskErr
			}

			var currentLock SystemTaskLock
			lockErr := LockForUpdate(tx).
				Where(
					"type = ? AND task_id = ? AND locked_by = ? AND locked_until < ?",
					staleLock.Type,
					staleLock.TaskID,
					staleLock.LockedBy,
					now,
				).
				First(&currentLock).Error
			if errors.Is(lockErr, gorm.ErrRecordNotFound) {
				return nil
			}
			if lockErr != nil {
				return lockErr
			}

			if err := tx.Delete(&currentLock).Error; err != nil {
				return err
			}
			if taskErr == nil &&
				task.Status == SystemTaskStatusRunning &&
				task.LockedBy == currentLock.LockedBy {
				return markSystemTaskLeaseExpiredWithTx(tx, task.TaskID)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return RecoverOrphanedRunningSystemTasks(now)
}

// RecoverOrphanedRunningSystemTasks closes the failure window where a runner
// loses or releases its lock but the task-row terminal update cannot be saved.
// Such a row must not permanently block the active-task key.
func RecoverOrphanedRunningSystemTasks(now int64) error {
	var runningTasks []*SystemTask
	if err := DB.
		Select("id", "task_id", "locked_by", "status").
		Where("status = ?", SystemTaskStatusRunning).
		Find(&runningTasks).Error; err != nil {
		return err
	}

	for _, candidate := range runningTasks {
		if candidate == nil {
			continue
		}
		if err := DB.Transaction(func(tx *gorm.DB) error {
			var task SystemTask
			if err := LockForUpdate(tx).
				Where("id = ? AND status = ?", candidate.ID, SystemTaskStatusRunning).
				First(&task).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}

			var lock SystemTaskLock
			err := tx.
				Where("task_id = ? AND locked_by = ? AND locked_until >= ?", task.TaskID, task.LockedBy, now).
				First(&lock).Error
			if err == nil {
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}

			return tx.Model(&SystemTask{}).
				Where("id = ? AND status = ? AND locked_by = ?", task.ID, SystemTaskStatusRunning, task.LockedBy).
				Updates(map[string]any{
					"status":     SystemTaskStatusFailed,
					"active_key": nil,
					"error":      "task lease missing",
					"updated_at": common.GetTimestamp(),
				}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func ReleaseSystemTaskLock(taskID string, lockedBy string) error {
	result := DB.Where("task_id = ? AND locked_by = ?", taskID, lockedBy).Delete(&SystemTaskLock{})
	return result.Error
}

func FinishSystemTask(taskID string, lockedBy string, status SystemTaskStatus, resultPayload any, errorMessage string) error {
	resultText, err := marshalSystemTaskJSON(resultPayload)
	if err != nil {
		return err
	}
	taskID = strings.TrimSpace(taskID)
	lockedBy = strings.TrimSpace(lockedBy)
	if taskID == "" {
		return errors.New("task id is required")
	}
	if lockedBy == "" {
		return errors.New("runner id is required")
	}
	switch status {
	case SystemTaskStatusSucceeded, SystemTaskStatusFailed, SystemTaskStatusCancelled:
	default:
		return errors.New("invalid terminal system task status")
	}
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		var task SystemTask
		if err := LockForUpdate(tx).Where("task_id = ?", taskID).First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSystemTaskLockLost
			}
			return err
		}

		if task.LockedBy == lockedBy &&
			(task.Status == SystemTaskStatusCancelled ||
				task.Status == SystemTaskStatusSucceeded ||
				task.Status == SystemTaskStatusFailed) {
			if task.Status == SystemTaskStatusCancelled &&
				resultText != "" &&
				strings.TrimSpace(task.Result) == "" {
				if err := tx.Model(&SystemTask{}).
					Where("id = ?", task.ID).
					Updates(map[string]any{
						"result":     resultText,
						"updated_at": now,
					}).Error; err != nil {
					return err
				}
			}
			return tx.Where("task_id = ? AND locked_by = ?", taskID, lockedBy).
				Delete(&SystemTaskLock{}).Error
		}

		if task.Status != SystemTaskStatusRunning || task.LockedBy != lockedBy {
			return ErrSystemTaskLockLost
		}

		var lock SystemTaskLock
		if err := LockForUpdate(tx).
			Where(
				"task_id = ? AND locked_by = ? AND locked_until >= ?",
				taskID,
				lockedBy,
				now,
			).
			First(&lock).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSystemTaskLockLost
			}
			return err
		}

		result := tx.Model(&SystemTask{}).
			Where("id = ? AND status = ? AND locked_by = ?", task.ID, SystemTaskStatusRunning, lockedBy).
			Updates(map[string]any{
				"status":     status,
				"active_key": nil,
				"result":     resultText,
				"error":      errorMessage,
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrSystemTaskLockLost
		}
		return tx.Where("task_id = ? AND locked_by = ?", taskID, lockedBy).
			Delete(&SystemTaskLock{}).Error
	})
}

func CleanupFinishedSystemTasks(cutoff int64, limit int) (int64, error) {
	if cutoff <= 0 {
		return 0, nil
	}
	if limit <= 0 {
		limit = 1000
	}
	var ids []int64
	err := DB.Model(&SystemTask{}).
		Select("id").
		Where("status IN ?", []SystemTaskStatus{SystemTaskStatusSucceeded, SystemTaskStatusFailed, SystemTaskStatusCancelled}).
		Where("updated_at < ?", cutoff).
		Order("id ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	result := DB.Where("id IN ?", ids).Delete(&SystemTask{})
	return result.RowsAffected, result.Error
}

func (task *SystemTask) DecodePayload(v any) error {
	return decodeSystemTaskJSONString(task.Payload, v)
}

func (task *SystemTask) DecodeState(v any) error {
	return decodeSystemTaskJSONString(task.State, v)
}

func (task *SystemTask) ToResponse() SystemTaskResponse {
	return SystemTaskResponse{
		ID:        task.ID,
		TaskID:    task.TaskID,
		Type:      task.Type,
		Status:    task.Status,
		ActiveKey: task.ActiveKey,
		Payload:   decodeSystemTaskJSONValue(task.Payload),
		State:     decodeSystemTaskJSONValue(task.State),
		Result:    decodeSystemTaskJSONValue(task.Result),
		Error:     task.Error,
		LockedBy:  task.LockedBy,
		CreatedAt: task.CreatedAt,
		UpdatedAt: task.UpdatedAt,
	}
}

func activeSystemTaskStatuses() []string {
	return []string{string(SystemTaskStatusPending), string(SystemTaskStatusRunning)}
}

func normalizeSystemTaskLookupValues(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func marshalSystemTaskJSON(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	data, err := common.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeSystemTaskJSONString(data string, v any) error {
	if data == "" {
		return nil
	}
	return common.UnmarshalJsonStr(data, v)
}

func decodeSystemTaskJSONValue(data string) any {
	if data == "" {
		return nil
	}
	var value any
	if err := common.UnmarshalJsonStr(data, &value); err != nil {
		return data
	}
	return value
}
