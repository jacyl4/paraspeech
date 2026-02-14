package voice

import "fmt"

type OpenAIAdapter struct{}

func (a *OpenAIAdapter) MapVoice(p *VoiceProfile) string {
	if p.Voice == "" {
		return "nova"
	}
	return p.Voice
}

func (a *OpenAIAdapter) MapInstructions(p *VoiceProfile) string {
	if p.Emotion == "" || p.Emotion == "neutral" {
		return ""
	}
	style := p.Style
	if style == "" {
		style = "conversational"
	}
	return fmt.Sprintf("Speak in a %s tone with %s style.", p.Emotion, style)
}

func (a *OpenAIAdapter) MapSpeed(p *VoiceProfile) float64 {
	if p.Speed <= 0 {
		return 1.0
	}
	return p.Speed
}

func (a *OpenAIAdapter) SupportsEmotion() bool {
	return true
}
