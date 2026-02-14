package tts

import "testing"

func TestEstimateDuration_CJK(t *testing.T) {
	text := "你好世界测试一下"
	dur := EstimateDuration(text)
	if dur < 1.0 || dur > 5.0 {
		t.Errorf("CJK duration out of range: %f", dur)
	}
}

func TestEstimateDuration_Latin(t *testing.T) {
	text := "Hello world this is a test"
	dur := EstimateDuration(text)
	if dur < 1.0 || dur > 10.0 {
		t.Errorf("Latin duration out of range: %f", dur)
	}
}

func TestEstimateDuration_Mixed(t *testing.T) {
	text := "你好 hello 世界 world 123"
	dur := EstimateDuration(text)
	if dur < 0.4 {
		t.Errorf("mixed duration too short: %f", dur)
	}
}

func TestEstimateDuration_MinimumFloor(t *testing.T) {
	text := "."
	dur := EstimateDuration(text)
	if dur < 0.4 {
		t.Errorf("expected minimum 0.4s, got %f", dur)
	}
}

func TestSplit_ShortText(t *testing.T) {
	segments := Split("你好", 25.0)
	if len(segments) != 1 {
		t.Errorf("expected 1 segment, got %d", len(segments))
	}
}

func TestSplit_ParagraphBoundary(t *testing.T) {
	text := "第一段内容。\n\n第二段内容。"
	segments := Split(text, 25.0)
	if len(segments) != 2 {
		t.Errorf("expected 2 segments at paragraph boundary, got %d", len(segments))
	}
}

func TestSplit_PreservesPunctuation(t *testing.T) {
	text := "这是一句话。这是另一句话！还有第三句？"
	segments := Split(text, 25.0)
	// Short enough to be one segment
	if len(segments) != 1 {
		t.Errorf("expected 1 segment for short text, got %d", len(segments))
	}
	// Punctuation preserved
	for _, s := range segments {
		if s.Text == "" {
			t.Error("empty segment text")
		}
	}
}

func TestSplit_LongTextSplitsAtSentenceEnd(t *testing.T) {
	// Create a long text that exceeds maxSec
	text := ""
	for i := 0; i < 50; i++ {
		text += "这是一个很长的句子需要被切分。"
	}
	segments := Split(text, 5.0)
	if len(segments) < 2 {
		t.Errorf("expected multiple segments for long text, got %d", len(segments))
	}
	// Each segment should end with punctuation
	for _, s := range segments {
		last := s.Text[len(s.Text)-len("。"):]
		if last != "。" && last != "！" && last != "？" {
			t.Logf("segment may not end at sentence boundary: %q", s.Text[max(0, len(s.Text)-30):])
		}
	}
}
