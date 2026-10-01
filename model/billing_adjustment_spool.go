package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const billingAdjustmentMemoryCap = 10000

var (
	billingAdjustmentMemoryMu sync.Mutex
	billingAdjustmentMemory   = map[string]BillingAdjustmentRequest{}
)

func billingAdjustmentSpoolDirectory(create bool) (string, error) {
	dir := strings.TrimSpace(BillingAdjustmentSpoolDir)
	if dir == "" {
		if common.LogDir == nil || strings.TrimSpace(*common.LogDir) == "" {
			return "", os.ErrNotExist
		}
		dir = filepath.Join(*common.LogDir, "billing-adjustments")
	}
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func billingAdjustmentFallbackDirectory() string {
	if dir := strings.TrimSpace(BillingAdjustmentFallbackSpoolDir); dir != "" {
		return dir
	}
	return filepath.Join(os.TempDir(), "newapi-billing-adjustments")
}

func billingAdjustmentSpoolName(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:]) + ".json"
}

func writeBillingAdjustmentSpoolFile(dir, key string, body []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".billing-adjustment-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, billingAdjustmentSpoolName(key))); err != nil {
		return err
	}
	committed = true
	dirFile, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer dirFile.Close()
	return dirFile.Sync()
}

func rememberBillingAdjustmentInMemory(req BillingAdjustmentRequest) bool {
	billingAdjustmentMemoryMu.Lock()
	defer billingAdjustmentMemoryMu.Unlock()
	if _, exists := billingAdjustmentMemory[req.IdempotencyKey]; !exists && len(billingAdjustmentMemory) >= billingAdjustmentMemoryCap {
		return false
	}
	billingAdjustmentMemory[req.IdempotencyKey] = req
	return true
}

func forgetUnpersistedBillingAdjustment(key string) {
	billingAdjustmentMemoryMu.Lock()
	defer billingAdjustmentMemoryMu.Unlock()
	delete(billingAdjustmentMemory, key)
}

// ForgetUnpersistedBillingAdjustment drops one staged settle request after the
// pending ledger row is durable. Keeping the spool would charge it again on recovery.
func ForgetUnpersistedBillingAdjustment(key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	forgetUnpersistedBillingAdjustment(key)
	name := billingAdjustmentSpoolName(key)
	if dir, err := billingAdjustmentSpoolDirectory(false); err == nil && strings.TrimSpace(dir) != "" {
		_ = os.Remove(filepath.Join(dir, name))
	}
	_ = os.Remove(filepath.Join(billingAdjustmentFallbackDirectory(), name))
}

func snapshotUnpersistedBillingAdjustments(limit int) []BillingAdjustmentRequest {
	billingAdjustmentMemoryMu.Lock()
	defer billingAdjustmentMemoryMu.Unlock()
	if limit <= 0 || len(billingAdjustmentMemory) == 0 {
		return nil
	}
	keys := make([]string, 0, len(billingAdjustmentMemory))
	for key := range billingAdjustmentMemory {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	out := make([]BillingAdjustmentRequest, 0, len(keys))
	for _, key := range keys {
		out = append(out, billingAdjustmentMemory[key])
	}
	return out
}

// ClearUnpersistedBillingAdjustments drops process-local settle requests.
// Tests call it so one failed case cannot be recovered by the next case.
func ClearUnpersistedBillingAdjustments() {
	billingAdjustmentMemoryMu.Lock()
	defer billingAdjustmentMemoryMu.Unlock()
	billingAdjustmentMemory = map[string]BillingAdjustmentRequest{}
}

// RememberUnpersistedBillingAdjustment stores one settle request until the
// pending ledger row can be written. The same key is overwritten in place.
// A primary disk failure falls back to the temp directory, then to memory.
func RememberUnpersistedBillingAdjustment(req BillingAdjustmentRequest) error {
	if err := req.validate(); err != nil {
		return err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	var first error
	if dir, dirErr := billingAdjustmentSpoolDirectory(false); dirErr != nil {
		if !errors.Is(dirErr, os.ErrNotExist) {
			first = dirErr
		}
	} else if writeErr := writeBillingAdjustmentSpoolFile(dir, req.IdempotencyKey, body); writeErr != nil {
		first = writeErr
	} else {
		return nil
	}
	if writeErr := writeBillingAdjustmentSpoolFile(billingAdjustmentFallbackDirectory(), req.IdempotencyKey, body); writeErr != nil {
		if first == nil {
			first = writeErr
		}
	} else {
		return nil
	}
	if rememberBillingAdjustmentInMemory(req) {
		return nil
	}
	if first != nil {
		return first
	}
	return errors.New("billing adjustment spool is unavailable")
}

// RecoverUnpersistedBillingAdjustments copies spooled settle requests into
// pending ledger rows. Applying those rows is a separate recovery step.
func RecoverUnpersistedBillingAdjustments(limit int) error {
	if limit <= 0 {
		limit = 100
	}
	var first error
	recovered, err := recoverMemoryBillingAdjustments(limit)
	if err != nil {
		first = err
	}
	for _, dir := range billingAdjustmentRecoveryDirectories() {
		if recovered >= limit {
			break
		}
		n, dirErr := recoverBillingAdjustmentSpoolDirectory(dir, limit-recovered)
		recovered += n
		if dirErr != nil && first == nil {
			first = dirErr
		}
	}
	return first
}

func billingAdjustmentRecoveryDirectories() []string {
	dirs := make([]string, 0, 2)
	if dir, err := billingAdjustmentSpoolDirectory(false); err == nil && strings.TrimSpace(dir) != "" {
		dirs = append(dirs, dir)
	}
	if fallback := strings.TrimSpace(billingAdjustmentFallbackDirectory()); fallback != "" {
		dirs = append(dirs, fallback)
	}
	return dirs
}

func recoverMemoryBillingAdjustments(limit int) (int, error) {
	var first error
	recovered := 0
	for _, req := range snapshotUnpersistedBillingAdjustments(limit) {
		if recovered >= limit {
			break
		}
		recovered++
		if err := EnsurePendingBillingAdjustment(req); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		forgetUnpersistedBillingAdjustment(req.IdempotencyKey)
	}
	return recovered, first
}

func recoverBillingAdjustmentSpoolDirectory(dir string, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	var first error
	recovered := 0
	for _, name := range names {
		if recovered >= limit {
			break
		}
		recovered++
		path := filepath.Join(dir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		var req BillingAdjustmentRequest
		if err := json.Unmarshal(body, &req); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err := EnsurePendingBillingAdjustment(req); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err := os.Remove(path); err != nil && first == nil {
			first = err
		}
	}
	return recovered, first
}
