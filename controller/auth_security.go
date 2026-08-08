package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// continueAfterCommittedUserAuthStateError preserves the post-commit security
// chain when only cache publication failed. The cache reader falls back to the
// database and heals the cache; callers still finish session rotation/revocation.
func continueAfterCommittedUserAuthStateError(operation string, err error) bool {
	if err == nil {
		return true
	}
	if !model.IsCommittedUserAuthStateError(err) {
		return false
	}
	common.SysError(fmt.Sprintf("%s committed but user auth cache publication failed: %v", operation, err))
	return true
}
