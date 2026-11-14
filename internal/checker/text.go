// text.go — chuyển formatted text (plaintext/html/markdown) của payload
// marketing sang text thuần cho popup/log. Port hành vi của app gốc:
//   - html: marketingLabel (marketingPopupAdapter.ts) strip node
//     script/style/iframe/object/svg/math/template rồi lấy textContent.
//   - markdown: app render bằng `marked`; popup native chỉ cần text
//     đọc được nên strip nhẹ syntax, giữ cấu trúc dòng.
package checker

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// formattedText đọc node {format, content} của payload (marketingTouch.ts:
// format plaintext|html|markdown, app gốc chấp nhận format rỗng ở banner
// và coi như plaintext).
func formattedText(node any) (string, string) {
	m, ok := node.(map[string]any)
	if !ok {
		return "plaintext", ""
	}
	format, _ := m["format"].(string)
	if format == "" {
		format = "plaintext"
	}
	content, _ := m["content"].(string)
	return format, strings.TrimSpace(content)
}

// plainText chuyển formatted text sang text thuần hiển thị được.
func plainText(format, content string) string {
	switch format {
	case "html":
		return htmlStripTags(content)
	case "markdown":
		return markdownStrip(content)
	default:
		return content
	}
}

// htmlStripTags strip HTML về text thuần. Node trong allow-list của app
// (b/strong/ul/li/a...) giữ text bên trong; node nguy hiểm hoặc render
// đặc biệt (script/style/iframe/object/svg/math/template) bị xoá cả cây.
func htmlStripTags(content string) string {
	root, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return content // parse lỗi thì giữ nguyên text gốc, không đoán
	}
	removeDisallowedNodes(root)
	var sb strings.Builder
	extractHTMLText(root, &sb)
	return strings.TrimSpace(sb.String())
}

func removeDisallowedNodes(root *html.Node) {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		child := n.FirstChild
		for child != nil {
			next := child.NextSibling
			if child.Type == html.ElementNode && disallowedElement(child.Data) {
				n.RemoveChild(child)
				child = next
				continue
			}
			walk(child)
			child = next
		}
	}
	walk(root)
}

func disallowedElement(name string) bool {
	switch name {
	case "script", "style", "iframe", "object", "svg", "math", "template":
		return true
	}
	return false
}

// extractHTMLText lấy textContent; br/p/div/li/tr/h1-h6... thêm xuống
// dòng để các khối không dính vào nhau thành một câu dài.
func extractHTMLText(n *html.Node, sb *strings.Builder) {
	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "br" {
			sb.WriteString("\n")
			continue
		}
		extractHTMLText(c, sb)
	}
	if n.Type == html.ElementNode && blockElement(n.Data) {
		sb.WriteString("\n")
	}
}

func blockElement(name string) bool {
	switch name {
	case "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "pre", "hr", "table":
		return true
	}
	return false
}

var (
	// Go regexp (RE2) không có backreference nên mỗi dạng marker viết
	// riêng 2 nhánh thay vì (\*\*|__)(...)\1. Italic chỉ xử lý dạng
	// *...*; dạng _..._ bỏ qua để không phá snake_case.
	mdLink   = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
	mdCode   = regexp.MustCompile("`([^`]*)`")
	mdBold   = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	mdItalic = regexp.MustCompile(`\*([^*\n]+)\*`)
)

// markdownStrip bỏ syntax markdown nhẹ nhàng, giữ nội dung + xuống dòng.
func markdownStrip(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue // bỏ fence marker, giữ nội dung giữa 2 fence
		}
		if !inFence {
			trimmed = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			line = trimmed
		}
		out = append(out, line)
	}
	text := strings.Join(out, "\n")
	text = mdLink.ReplaceAllString(text, "$1")
	text = mdCode.ReplaceAllString(text, "$1")
	text = mdBold.ReplaceAllString(text, "${1}${2}")
	text = mdItalic.ReplaceAllString(text, "$1")
	return strings.TrimSpace(text)
}
