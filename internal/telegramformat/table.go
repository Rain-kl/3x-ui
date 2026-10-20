package telegramformat

import (
	"html"
	"strconv"
	"strings"

	nethtml "golang.org/x/net/html"
)

func renderTable(table *nethtml.Node, output *strings.Builder, renderCell func(*nethtml.Node) string) {
	var rows [][]*nethtml.Node
	var collect func(*nethtml.Node)
	collect = func(node *nethtml.Node) {
		if node != table && node.Type == nethtml.ElementNode && node.Data == "table" {
			return
		}
		if node.Type == nethtml.ElementNode && node.Data == "tr" {
			var cells []*nethtml.Node
			for cell := node.FirstChild; cell != nil; cell = cell.NextSibling {
				if cell.Type == nethtml.ElementNode && (cell.Data == "th" || cell.Data == "td") {
					cells = append(cells, cell)
				}
			}
			if len(cells) > 0 {
				rows = append(rows, cells)
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	collect(table)
	if len(rows) == 0 {
		return
	}
	var headers []string
	if rows[0][0].Data == "th" {
		for _, cell := range rows[0] {
			headers = append(headers, tableLabel(cell))
		}
		rows = rows[1:]
	}
	if len(rows) == 0 {
		output.WriteString(html.EscapeString(strings.Join(headers, " · ")))
		output.WriteByte('\n')
		return
	}
	if output.Len() > 0 && !strings.HasSuffix(output.String(), "\n") {
		output.WriteByte('\n')
	}
	for index, row := range rows {
		if index > 0 {
			output.WriteByte('\n')
		}
		for column, cell := range row {
			content := renderCell(cell)
			if content == "" {
				content = "—"
			}
			if column == 0 {
				if !strings.Contains(content, "<") {
					content = "<b>" + content + "</b>"
				}
			} else {
				label := strconv.Itoa(column + 1)
				if column < len(headers) && headers[column] != "" {
					label = headers[column]
				}
				output.WriteString("<b>" + html.EscapeString(label) + "：</b>")
			}
			output.WriteString(content)
			output.WriteByte('\n')
		}
	}
}

func tableLabel(node *nethtml.Node) string {
	var text strings.Builder
	var collect func(*nethtml.Node)
	collect = func(node *nethtml.Node) {
		if node.Type == nethtml.TextNode {
			text.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	collect(node)
	return strings.Join(strings.Fields(text.String()), " ")
}
