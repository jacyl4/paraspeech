package tts

import (
	"strings"
	"testing"
)

func TestSanitize_FencedCode(t *testing.T) {
	input := "前面\n```python\nprint('hello')\n```\n后面"
	result := Sanitize(input)
	if strings.Contains(result, "print") {
		t.Errorf("fenced code not removed: %q", result)
	}
	if !strings.Contains(result, "前面") || !strings.Contains(result, "后面") {
		t.Errorf("surrounding text lost: %q", result)
	}
}

func TestSanitize_InlineCode(t *testing.T) {
	result := Sanitize("使用 `fmt.Println` 函数")
	if strings.Contains(result, "fmt.Println") {
		t.Errorf("inline code not removed: %q", result)
	}
}

func TestSanitize_MarkdownLink(t *testing.T) {
	result := Sanitize("请看 [文档](https://example.com) 了解更多")
	if strings.Contains(result, "https://") {
		t.Errorf("URL not removed: %q", result)
	}
	if !strings.Contains(result, "文档") {
		t.Errorf("link text lost: %q", result)
	}
}

func TestSanitize_URL(t *testing.T) {
	result := Sanitize("访问 https://example.com/path 获取")
	if strings.Contains(result, "https://") {
		t.Errorf("URL not removed: %q", result)
	}
}

func TestSanitize_MarkdownPunctuation(t *testing.T) {
	result := Sanitize("**粗体** *斜体* ~~删除~~")
	if strings.Contains(result, "**") || strings.Contains(result, "~~") {
		t.Errorf("markdown punctuation not removed: %q", result)
	}
}

func TestSanitize_ControlTags(t *testing.T) {
	result := Sanitize("正文 [[session_end]] 结束")
	if strings.Contains(result, "[[") {
		t.Errorf("control tag not removed: %q", result)
	}
}

func TestSanitize_EmptyInput(t *testing.T) {
	result := Sanitize("")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}
