package common

var TrustQuota = 0

func GetTrustQuota() int {
	if TrustQuota <= 0 {
		return 0
	}
	return TrustQuota
}
