package constant

type TaskPlatform string

const (
	TaskPlatformSuno       TaskPlatform = "suno"
	TaskPlatformMidjourney              = "mj"
	TaskPlatformImage                   = "image"
)

const (
	SunoActionMusic  = "MUSIC"
	SunoActionLyrics = "LYRICS"

	TaskActionGenerate          = "generate"
	TaskActionTextGenerate      = "textGenerate"
	TaskActionFirstTailGenerate = "firstTailGenerate"
	TaskActionReferenceGenerate = "referenceGenerate"
	TaskActionRemix             = "remixGenerate"
	TaskActionImageGeneration   = "image_generation"
	TaskActionImageEdit         = "image_edit"

	// Canonical task-plugin action names. The legacy names above remain
	// accepted by existing provider adaptors and are normalized at the
	// plugin boundary.
	TaskActionImageToVideo     = "image_to_video"
	TaskActionTextToVideo      = "text_to_video"
	TaskActionFirstTailToVideo = "first_tail_to_video"
	TaskActionReferenceToVideo = "reference_to_video"
	TaskActionRemixCanonical   = "remix"
)

var SunoModel2Action = map[string]string{
	"suno_music":  SunoActionMusic,
	"suno_lyrics": SunoActionLyrics,
}

var legacyTaskActionAliases = map[string]string{
	TaskActionGenerate:          TaskActionImageToVideo,
	TaskActionTextGenerate:      TaskActionTextToVideo,
	TaskActionFirstTailGenerate: TaskActionFirstTailToVideo,
	TaskActionReferenceGenerate: TaskActionReferenceToVideo,
	TaskActionRemix:             TaskActionRemixCanonical,
}

// TaskPluginEnabled controls the master switch for sandboxed task plugins.
var TaskPluginEnabled = true

// TaskPluginOverrideEnabled controls whether database plugin overrides are
// active; factory plugins remain available when overrides are disabled.
var TaskPluginOverrideEnabled = true

func NormalizeTaskAction(action string) string {
	if canonical, ok := legacyTaskActionAliases[action]; ok {
		return canonical
	}
	return action
}
