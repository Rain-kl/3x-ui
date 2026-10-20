package telegramformat

import (
	"bytes"
	"html"
	"net/url"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	nethtml "golang.org/x/net/html"
)

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithRendererOptions(goldhtml.WithUnsafe()))

// Markdown renders through a strict Telegram HTML subset; raw HTML never bypasses this filter.
func Markdown(text string) string {
	var rendered bytes.Buffer
	if err := markdown.Convert([]byte(text), &rendered); err != nil {
		return html.EscapeString(text)
	}
	doc, err := nethtml.Parse(&rendered)
	if err != nil {
		return html.EscapeString(text)
	}
	var output strings.Builder
	var walk func(*nethtml.Node, bool, *strings.Builder)
	walk = func(node *nethtml.Node, quoted bool, output *strings.Builder) {
		if node.Type == nethtml.TextNode {
			output.WriteString(html.EscapeString(node.Data))
			return
		}
		if node.Data == "script" || node.Data == "style" || node.Type == nethtml.CommentNode {
			return
		}
		open, close := "", ""
		switch node.Data {
		case "strong", "b", "h1", "h2", "h3", "h4", "h5", "h6":
			open, close = "<b>", "</b>"
		case "em", "i":
			open, close = "<i>", "</i>"
		case "del", "s", "strike":
			open, close = "<s>", "</s>"
		case "u", "ins":
			open, close = "<u>", "</u>"
		case "code":
			open, close = "<code>", "</code>"
		case "pre":
			open, close = "<pre>", "</pre>"
		case "blockquote":
			if !quoted {
				open, close = "<blockquote>", "</blockquote>"
			}
		case "a":
			for _, attr := range node.Attr {
				if attr.Key != "href" {
					continue
				}
				target, err := url.Parse(attr.Val)
				if err == nil && (target.Scheme == "http" || target.Scheme == "https" || target.Scheme == "tg" || target.Scheme == "mailto") {
					open, close = `<a href="`+html.EscapeString(attr.Val)+`">`, "</a>"
				}
			}
		case "table":
			renderTable(node, output, func(cell *nethtml.Node) string {
				var content strings.Builder
				for child := cell.FirstChild; child != nil; child = child.NextSibling {
					walk(child, quoted, &content)
				}
				return strings.TrimSpace(content.String())
			})
			return
		case "br":
			output.WriteByte('\n')
		case "hr":
			output.WriteString("\n────────\n")
		case "li":
			if node.Parent != nil && node.Parent.Data == "ol" {
				index := 1
				for previous := node.PrevSibling; previous != nil; previous = previous.PrevSibling {
					if previous.Data == "li" {
						index++
					}
				}
				output.WriteString(strconv.Itoa(index) + ". ")
			} else {
				output.WriteString("• ")
			}
		case "img":
			for _, attr := range node.Attr {
				if attr.Key == "alt" {
					output.WriteString(html.EscapeString(attr.Val))
				}
			}
		}
		output.WriteString(open)
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, quoted || node.Data == "blockquote", output)
		}
		output.WriteString(close)
		switch node.Data {
		case "p", "pre", "blockquote", "li", "h1", "h2", "h3", "h4", "h5", "h6":
			output.WriteByte('\n')
		}
	}
	walk(doc, false, &output)
	result := strings.Trim(output.String(), "\n")
	if result == "" && text != "" {
		return html.EscapeString(text)
	}
	return result
}

// Legacy preserves established administrator HTML templates while accepting Markdown text.
func Legacy(text string) string {
	for _, tag := range []string{"<b>", "<i>", "<u>", "<s>", "<code>", "<pre>", "<a ", "<blockquote>"} {
		if strings.Contains(text, tag) {
			return text
		}
	}
	return Markdown(text)
}

// Chunks keeps tags balanced and counts decoded UTF-16 units instead of encoded HTML bytes.
func Chunks(rendered string, limit int) []string {
	if limit < 2 {
		limit = 3500
	}
	type tag struct{ start, end string }
	var stack []tag
	var result []string
	var part strings.Builder
	length := 0
	flush := func() {
		for i := len(stack) - 1; i >= 0; i-- {
			part.WriteString(stack[i].end)
		}
		result = append(result, part.String())
		part.Reset()
		for _, tag := range stack {
			part.WriteString(tag.start)
		}
		length = 0
	}
	tokens := nethtml.NewTokenizer(strings.NewReader(rendered))
	for {
		switch tokens.Next() {
		case nethtml.ErrorToken:
			if length > 0 {
				flush()
			}
			return result
		case nethtml.StartTagToken:
			text := string(tokens.Raw())
			token := tokens.Token()
			stack = append(stack, tag{text, "</" + token.Data + ">"})
			part.WriteString(text)
		case nethtml.EndTagToken:
			part.Write(tokens.Raw())
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case nethtml.TextToken:
			for _, ch := range tokens.Token().Data {
				units := 1
				if ch > 0xffff {
					units = 2
				}
				if length+units > limit {
					flush()
				}
				part.WriteString(html.EscapeString(string(ch)))
				length += units
			}
		}
	}
}
