package main

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// 富文本（图文课时）白名单过滤：老师提交的 HTML 只保留安全标签与属性，
// 防止存储型 XSS（脚本、事件属性、javascript: 链接、iframe 等一律剔除）。

var richAllowedTags = map[string]bool{
	// html/body 只是文档结构，最终只渲染 body 的子节点
	"html": true, "body": true,
	"p": true, "br": true, "div": true, "span": true, "hr": true,
	"strong": true, "b": true, "em": true, "i": true, "u": true, "s": true, "del": true, "ins": true,
	"ul": true, "ol": true, "li": true, "blockquote": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"a": true, "img": true, "pre": true, "code": true,
	"table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true,
	"figure": true, "figcaption": true,
}

// 危险标签：连同子节点一起删除
var richDropTags = map[string]bool{
	"script": true, "style": true, "iframe": true, "frame": true, "frameset": true,
	"object": true, "embed": true, "link": true, "meta": true, "title": true, "head": true,
	"form": true, "input": true, "button": true, "textarea": true, "select": true, "option": true,
	"svg": true, "math": true, "audio": true, "video": true, "source": true, "canvas": true,
	"noscript": true, "template": true, "base": true,
}

var safeStyleRe = regexp.MustCompile(`^(?:[a-zA-Z-]+\s*:\s*[^;{}<>"'\\]+;?\s*)+$`)

func safeURL(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	if s == "" {
		return false
	}
	for _, p := range []string{"http://", "https://", "//", "/", "#", "mailto:"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func safeImgSrc(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	if strings.HasPrefix(s, "data:image/") {
		return true
	}
	return safeURL(v)
}

func safeStyle(v string) bool {
	s := strings.ToLower(v)
	if strings.Contains(s, "expression") || strings.Contains(s, "javascript:") ||
		strings.Contains(s, "url(") || strings.Contains(s, "behavior") || strings.Contains(s, "@import") {
		return false
	}
	return safeStyleRe.MatchString(v)
}

func removeDangerous(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode && richDropTags[strings.ToLower(c.Data)] {
			n.RemoveChild(c)
		} else {
			removeDangerous(c)
		}
		c = next
	}
}

// unwrapDisallowed 把不在白名单里的标签展开成其子节点；返回是否做过修改
func unwrapDisallowed(n *html.Node) bool {
	changed := false
	var targets []*html.Node
	var walk func(*html.Node)
	walk = func(p *html.Node) {
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && !richAllowedTags[strings.ToLower(c.Data)] {
				targets = append(targets, c)
				continue
			}
			walk(c)
		}
	}
	walk(n)
	for _, t := range targets {
		parent := t.Parent
		if parent == nil {
			continue
		}
		for t.FirstChild != nil {
			child := t.FirstChild
			t.RemoveChild(child)
			parent.InsertBefore(child, t)
		}
		parent.RemoveChild(t)
		changed = true
	}
	return changed
}

func cleanAttrs(n *html.Node) {
	if n.Type == html.ElementNode {
		tag := strings.ToLower(n.Data)
		keep := n.Attr[:0]
		for _, a := range n.Attr {
			key := strings.ToLower(a.Key)
			if a.Namespace != "" || strings.HasPrefix(key, "on") || key == "srcset" || key == "formaction" {
				continue
			}
			switch tag {
			case "a":
				if (key == "href" && safeURL(a.Val)) || key == "title" {
					keep = append(keep, a)
				}
			case "img":
				if (key == "src" && safeImgSrc(a.Val)) || key == "alt" || key == "title" {
					keep = append(keep, a)
				}
			default:
				if key == "style" && safeStyle(a.Val) {
					keep = append(keep, a)
				}
			}
		}
		n.Attr = keep
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		cleanAttrs(c)
	}
}

func findBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && strings.EqualFold(n.Data, "body") {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if b := findBody(c); b != nil {
			return b
		}
	}
	return nil
}

// sanitizeHTML 返回只含白名单标签的 HTML 片段
func sanitizeHTML(src string) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return ""
	}
	removeDangerous(doc)
	for i := 0; i < 8; i++ {
		if !unwrapDisallowed(doc) {
			break
		}
	}
	cleanAttrs(doc)
	body := findBody(doc)
	if body == nil {
		return ""
	}
	var b strings.Builder
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&b, c); err != nil {
			return ""
		}
	}
	return b.String()
}
