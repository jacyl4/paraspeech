package voice

type VoiceProfile struct {
	Voice   string
	Speed   float64
	Emotion string
	Pitch   string
	Style   string
	Custom  map[string]string
}

type ProviderAdapter interface {
	MapVoice(profile *VoiceProfile) string
	MapInstructions(profile *VoiceProfile) string
	MapSpeed(profile *VoiceProfile) float64
	SupportsEmotion() bool
}
