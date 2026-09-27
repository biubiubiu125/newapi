package system_setting

import (
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

type PasskeySettings struct {
	Enabled              bool   `json:"enabled"`
	RPDisplayName        string `json:"rp_display_name"`
	RPID                 string `json:"rp_id"`
	LegacyRPIDs          string `json:"legacy_rp_ids"`
	Origins              string `json:"origins"`
	AllowInsecureOrigin  bool   `json:"allow_insecure_origin"`
	UserVerification     string `json:"user_verification"`
	AttachmentPreference string `json:"attachment_preference"`
}

var ErrPasskeyRPIDInvalid = errors.New("Invalid Passkey domain. Enter a domain without a scheme, port, path or wildcard.")
var ErrPasskeyRPIDUnavailable = errors.New("This Passkey domain is not available on this website. Use its original website or another verification method.")

// EffectiveRPID preserves the existing default and port handling for settings
// created before explicit Passkey domains were introduced.
func (s PasskeySettings) EffectiveRPID() string {
	rpID := strings.TrimSpace(s.RPID)
	if rpID == "" {
		for origin := range strings.SplitSeq(s.Origins, ",") {
			if parsed, err := url.Parse(strings.TrimSpace(origin)); err == nil && parsed.Host != "" {
				rpID = parsed.Host
				break
			}
		}
	}
	if host, _, err := net.SplitHostPort(rpID); err == nil {
		return host
	}
	return rpID
}

func NormalizePasskeyRPID(value string, configuredOrigins ...string) (string, error) {
	rpID, err := idna.Lookup.ToASCII(strings.TrimSpace(value))
	rpID = strings.ToLower(rpID)
	if err != nil || rpID == "" || len(rpID) > 253 || strings.ContainsAny(rpID, ":/*@?#\\") || net.ParseIP(rpID) != nil {
		return "", ErrPasskeyRPIDInvalid
	}
	for label := range strings.SplitSeq(rpID, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrPasskeyRPIDInvalid
		}
		for _, c := range label {
			if c != '-' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
				return "", ErrPasskeyRPIDInvalid
			}
		}
	}
	if rpID == "localhost" {
		return rpID, nil
	}
	if _, err := publicsuffix.EffectiveTLDPlusOne(rpID); err == nil {
		return rpID, nil
	}
	// An unknown single-label hostname is usable only at its exact, explicitly
	// trusted HTTPS origin, never as a suffix of other hosts.
	_, icann := publicsuffix.PublicSuffix(rpID)
	if icann || strings.Contains(rpID, ".") {
		return "", ErrPasskeyRPIDInvalid
	}
	for _, origins := range configuredOrigins {
		for origin := range strings.SplitSeq(origins, ",") {
			parsed, err := url.Parse(strings.TrimSpace(origin))
			if err != nil || parsed.Scheme != "https" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
				continue
			}
			if strings.EqualFold(parsed.Hostname(), rpID) {
				return rpID, nil
			}
		}
	}
	return "", ErrPasskeyRPIDInvalid
}

func ParsePasskeyRPIDs(value string, configuredOrigins ...string) ([]string, error) {
	ids := []string{}
	for _, value := range strings.FieldsFunc(value, func(c rune) bool { return c == ',' || c == '\n' || c == '\r' }) {
		if strings.TrimSpace(value) == "" {
			continue
		}
		_, err := NormalizePasskeyRPID(value, configuredOrigins...)
		if err != nil {
			return nil, err
		}
		// Legacy credentials hash the exact RP ID supplied at registration.
		// Validate the domain without changing that historical value.
		id := strings.TrimSpace(value)
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s PasskeySettings) RelyingPartyIDs() []string {
	ids := []string{}
	if primary := s.EffectiveRPID(); primary != "" {
		ids = append(ids, primary)
	}
	legacy, err := ParsePasskeyRPIDs(s.LegacyRPIDs, s.Origins)
	if err != nil {
		return ids
	}
	for _, id := range legacy {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

var defaultPasskeySettings = PasskeySettings{
	Enabled:              false,
	RPDisplayName:        common.SystemName,
	RPID:                 "",
	Origins:              "",
	AllowInsecureOrigin:  false,
	UserVerification:     "preferred",
	AttachmentPreference: "",
}

func init() {
	config.GlobalConfig.Register("passkey", &defaultPasskeySettings)
}

func PasskeySettingsSnapshot() PasskeySettings {
	common.OptionMapRWMutex.RLock()
	settings := defaultPasskeySettings
	serverAddress := ServerAddress
	common.OptionMapRWMutex.RUnlock()
	return settings.WithDefaults(serverAddress)
}

func GetPasskeySettings() *PasskeySettings {
	snapshot := PasskeySettingsSnapshot()
	return &snapshot
}

// PasskeySettingsStoredCopy is the configured passkey row without implicit
// ServerAddress defaults. Domain updates need the blank stored origins so a
// server move can retain both the old and new websites.
func PasskeySettingsStoredCopy() PasskeySettings {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return defaultPasskeySettings
}

// MutablePasskeySettingsForTest returns the process passkey settings.
// Tests may edit the result. Production callers must use GetPasskeySettings,
// which returns a snapshot and does not observe later mutation of a copy.
func MutablePasskeySettingsForTest() *PasskeySettings {
	return &defaultPasskeySettings
}

func OverridePasskeySettingsForTest(settings PasskeySettings) func() {
	common.OptionMapRWMutex.Lock()
	previous := defaultPasskeySettings
	defaultPasskeySettings = settings
	common.OptionMapRWMutex.Unlock()
	return func() {
		common.OptionMapRWMutex.Lock()
		defaultPasskeySettings = previous
		common.OptionMapRWMutex.Unlock()
	}
}

func (s PasskeySettings) WithDefaults(serverAddress string) PasskeySettings {
	serverAddress = strings.TrimSpace(serverAddress)
	if strings.TrimSpace(s.RPID) == "" && serverAddress != "" {
		if parsed, err := url.Parse(serverAddress); err == nil && parsed.Host != "" {
			s.RPID = hostWithoutPort(parsed.Host)
		} else {
			s.RPID = hostWithoutPort(serverAddress)
		}
	} else {
		s.RPID = hostWithoutPort(s.RPID)
	}
	if s.Origins == "" || s.Origins == "[]" {
		s.Origins = serverAddress
	}
	return s
}

func hostWithoutPort(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if hostname, _, err := net.SplitHostPort(host); err == nil {
		return hostname
	}
	return host
}
