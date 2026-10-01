package common

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type verificationValue struct {
	code string
	time time.Time
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
)

// VerificationBackend stores one-time codes outside process memory so they
// survive restarts and are visible to every instance.
type VerificationBackend interface {
	Save(key, code, purpose string, expiresAt time.Time) error
	Match(key, code, purpose string, now time.Time) (bool, error)
	Consume(key, code, purpose string, now time.Time) (bool, error)
	Delete(key, purpose string) error
}

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10
var verificationBackend VerificationBackend
var VerificationValidMinutes = 10

func SetVerificationBackend(backend VerificationBackend) {
	verificationMutex.Lock()
	verificationBackend = backend
	verificationMutex.Unlock()
}

func currentVerificationBackend() VerificationBackend {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	return verificationBackend
}

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	if length > len(code) {
		length = len(code)
	}
	return code[:length]
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) error {
	key = strings.TrimSpace(key)
	code = strings.TrimSpace(code)
	purpose = strings.TrimSpace(purpose)
	if key == "" || code == "" || purpose == "" {
		return Localized("common.invalid_params")
	}
	expiresAt := time.Now().Add(time.Duration(VerificationValidMinutes) * time.Minute)
	if backend := currentVerificationBackend(); backend != nil {
		return backend.Save(key, code, purpose, expiresAt)
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	if verificationMap == nil {
		verificationMap = make(map[string]verificationValue)
	}
	verificationMap[purpose+key] = verificationValue{
		code: code,
		time: time.Now(),
	}
	if len(verificationMap) > verificationMapMaxSize {
		removeExpiredPairs()
	}
	return nil
}

func VerifyCodeWithKey(key string, code string, purpose string) bool {
	key = strings.TrimSpace(key)
	code = strings.TrimSpace(code)
	purpose = strings.TrimSpace(purpose)
	if key == "" || code == "" || purpose == "" {
		return false
	}
	now := time.Now()
	if backend := currentVerificationBackend(); backend != nil {
		matched, err := backend.Match(key, code, purpose, now)
		if err != nil {
			SysLog("verify verification code failed: " + err.Error())
			return false
		}
		return matched
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, okay := verificationMap[purpose+key]
	if !okay || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 {
		return false
	}
	return code == value.code
}

// ConsumeCodeWithKey verifies and atomically consumes a one-time code.
func ConsumeCodeWithKey(key string, code string, purpose string) bool {
	key = strings.TrimSpace(key)
	code = strings.TrimSpace(code)
	purpose = strings.TrimSpace(purpose)
	if key == "" || code == "" || purpose == "" {
		return false
	}
	now := time.Now()
	if backend := currentVerificationBackend(); backend != nil {
		consumed, err := backend.Consume(key, code, purpose, now)
		if err != nil {
			SysLog("consume verification code failed: " + err.Error())
			return false
		}
		return consumed
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	mapKey := purpose + key
	value, okay := verificationMap[mapKey]
	if !okay || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 || code != value.code {
		return false
	}
	delete(verificationMap, mapKey)
	return true
}

func DeleteKey(key string, purpose string) {
	key = strings.TrimSpace(key)
	purpose = strings.TrimSpace(purpose)
	if key == "" || purpose == "" {
		return
	}
	if backend := currentVerificationBackend(); backend != nil {
		if err := backend.Delete(key, purpose); err != nil {
			SysLog("delete verification code failed: " + err.Error())
		}
		return
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
