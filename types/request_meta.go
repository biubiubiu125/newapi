package types

import kittypes "github.com/QuantumNous/new-api/relaykit/types"

type FileType = kittypes.FileType
type TokenType = kittypes.TokenType
type TokenCountMeta = kittypes.TokenCountMeta
type FileMeta = kittypes.FileMeta
type RequestMeta = kittypes.RequestMeta

const (
	FileTypeImage = kittypes.FileTypeImage
	FileTypeAudio = kittypes.FileTypeAudio
	FileTypeVideo = kittypes.FileTypeVideo
	FileTypeFile  = kittypes.FileTypeFile
)

const (
	TokenTypeTextNumber = kittypes.TokenTypeTextNumber
	TokenTypeTokenizer  = kittypes.TokenTypeTokenizer
	TokenTypeImage      = kittypes.TokenTypeImage
)

var (
	NewFileMeta      = kittypes.NewFileMeta
	NewImageFileMeta = kittypes.NewImageFileMeta
)
