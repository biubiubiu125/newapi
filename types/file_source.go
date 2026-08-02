package types

import kittypes "github.com/QuantumNous/new-api/relaykit/types"

type FileSource = kittypes.FileSource
type URLSource = kittypes.URLSource
type Base64Source = kittypes.Base64Source
type CachedFileData = kittypes.CachedFileData

var (
	NewURLFileSource      = kittypes.NewURLFileSource
	NewBase64FileSource   = kittypes.NewBase64FileSource
	NewFileSourceFromData = kittypes.NewFileSourceFromData
	NewMemoryCachedData   = kittypes.NewMemoryCachedData
	NewDiskCachedData     = kittypes.NewDiskCachedData
)
