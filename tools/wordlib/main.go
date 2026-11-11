// wordlib：把词书 PDF 里末尾的单词表抽成 JSON 词库。
//
// 用法（在仓库根目录）：
//
//	go run ./tools/wordlib -in <PDF 文件 | 本地目录 | git 仓库地址> [-sub 关键字] [-last N] [-out vocab]
//
//	-in  单个 PDF、一个装 PDF 的目录，或 git 仓库地址（http(s)/ssh，浅克隆到临时目录）
//	-sub 只处理路径里含该关键字的 PDF（目录 / 仓库输入时有效）
//	-last 每本只解析末尾 N 页（词表一般在书末附录），0 = 全书
//	-out JSON 输出目录，默认 vocab/
//
// 输出：每本 PDF 一个 <书名>.json（word/phonetic/pos/meaning），外加一个 index.json 汇总。
// 词库只是先落盘备用，服务端目前不读取该目录；接入词书功能时再引用。
// 注意： extracted JSON 含词书内容，请自行确认版权与分发范围，默认不要提交到公开仓库。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ledongthuc/pdf"
)

// WordEntry 词库里的一个词条
type WordEntry struct {
	Word     string `json:"word"`
	Phonetic string `json:"phonetic,omitempty"`
	POS      string `json:"pos,omitempty"` // n. / v. / adj. …
	Meaning  string `json:"meaning,omitempty"`
}

// WordBook 一本词书的抽取结果
type WordBook struct {
	Book      string      `json:"book"`
	Source    string      `json:"source,omitempty"`
	Pages     string      `json:"pages,omitempty"`
	Count     int         `json:"count"`
	Entries   []WordEntry `json:"words"`
	Generated string      `json:"generated_at"`
}

func main() {
	in := flag.String("in", "", "PDF 文件 / 目录 / git 仓库地址（必填）")
	sub := flag.String("sub", "", "只处理路径含该关键字的 PDF")
	last := flag.Int("last", 10, "每本只解析末尾 N 页（0 = 全书）")
	out := flag.String("out", "vocab", "JSON 输出目录")
	bare := flag.Bool("bare", false, "保留没有释义的纯单词行")
	flag.Parse()
	if strings.TrimSpace(*in) == "" {
		fmt.Fprintln(os.Stderr, "需要 -in 参数：PDF 文件 / 目录 / git 仓库地址")
		flag.Usage()
		os.Exit(2)
	}
	src, cleanup, err := resolveSource(*in)
	if err != nil {
		fatal(err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	pdfs, err := collectPDFs(src, *sub)
	if err != nil {
		fatal(err)
	}
	if len(pdfs) == 0 {
		fatal(fmt.Errorf("没找到 PDF（-in=%s -sub=%q）", *in, *sub))
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}

	index := []map[string]any{}
	now := time.Now().Format(time.RFC3339)
	for _, path := range pdfs {
		book, err := extractBook(path, *in, *last, *bare, now)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[跳过] %s: %v\n", path, err)
			continue
		}
		if book.Count == 0 {
			fmt.Fprintf(os.Stderr, "[空]   %s: 没抽到词条（可能不是文本型 PDF，或词表不在末尾页）\n", path)
			continue
		}
		name := safeStem(strings.TrimSuffix(filepath.Base(path), ".pdf"))
		dst := filepath.Join(*out, name+".json")
		b, _ := json.MarshalIndent(book, "", "  ")
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("[ok]   %s -> %s（%d 词，%s）\n", filepath.Base(path), dst, book.Count, book.Pages)
		index = append(index, map[string]any{
			"book": book.Book, "file": filepath.Base(dst), "count": book.Count,
			"source": book.Source, "generated_at": now,
		})
	}
	if len(index) > 0 {
		b, _ := json.MarshalIndent(index, "", "  ")
		_ = os.WriteFile(filepath.Join(*out, "index.json"), b, 0o644)
		fmt.Printf("共 %d 本词书，索引：%s\n", len(index), filepath.Join(*out, "index.json"))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(1)
}

// resolveSource git 地址先浅克隆到临时目录，本地路径原样返回
func resolveSource(in string) (dir string, cleanup func(), err error) {
	lower := strings.ToLower(in)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "git@") || strings.HasSuffix(lower, ".git") {
		tmp, err := os.MkdirTemp("", "wordlib-clone-*")
		if err != nil {
			return "", nil, err
		}
		dst := filepath.Join(tmp, "repo")
		// 显式清掉全局代理配置：git 全局 http.proxy 常指向本机代理，挂了会连不上
		cmd := exec.Command("git", "-c", "http.proxy=", "-c", "https.proxy=", "clone", "--depth", "1", in, dst)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			os.RemoveAll(tmp)
			return "", nil, fmt.Errorf("克隆 %s 失败: %v\n%s", in, err, strings.TrimSpace(string(out)))
		}
		return dst, func() { os.RemoveAll(tmp) }, nil
	}
	st, err := os.Stat(in)
	if err != nil {
		return "", nil, err
	}
	if st.IsDir() {
		return in, nil, nil
	}
	return filepath.Dir(in), nil, nil
}

// collectPDFs 找出目录（递归）里符合条件的 PDF；单文件路径直接返回
func collectPDFs(src, sub string) ([]string, error) {
	st, err := os.Stat(src)
	if err == nil && !st.IsDir() && strings.EqualFold(filepath.Ext(src), ".pdf") {
		return []string{src}, nil
	}
	out := []string{}
	err = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".pdf") {
			return nil
		}
		if sub != "" && !strings.Contains(strings.ToLower(path), strings.ToLower(sub)) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	sort.Strings(out)
	return out, err
}

// extractBook 抽一本 PDF：解析末尾 lastN 页的词表行
func extractBook(path, source string, lastN int, bare bool, now string) (*WordBook, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	r, err := pdf.NewReader(f, st.Size())
	if err != nil {
		return nil, fmt.Errorf("打开 PDF 失败: %w", err)
	}
	total := r.NumPage()
	if total <= 0 {
		return nil, fmt.Errorf("PDF 没有页面")
	}
	first := 1
	if lastN > 0 && total > lastN {
		first = total - lastN + 1
	}
	seen := map[string]bool{}
	entries := []WordEntry{}
	for p := first; p <= total; p++ {
		page := r.Page(p)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue // 单页解析失败不影响其他页
		}
		for _, line := range strings.Split(text, "\n") {
			e, ok := parseWordLine(line, bare)
			if !ok {
				continue
			}
			key := strings.ToLower(e.Word)
			if seen[key] {
				continue
			}
			seen[key] = true
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Word < entries[j].Word })
	return &WordBook{
		Book:      strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Source:    source,
		Pages:     fmt.Sprintf("%d-%d/%d", first, total, total),
		Count:     len(entries),
		Entries:   entries,
		Generated: now,
	}, nil
}

var (
	leadingNumRe = regexp.MustCompile(`^\s*[\(（]?[0-9]{1,4}[\)）]?[\.、．．]?\s*`)
	phoneticRe   = regexp.MustCompile(`[/\[\(]([^/\[\]（）]{1,60})[/\]\)]`)
	posRe        = regexp.MustCompile(`^\s*((?:n|v|vt|vi|adj|adv|prep|conj|pron|art|num|interj|int|aux|abbr|pl|a)\.)\s*`)
	wordCharRe   = regexp.MustCompile(`^[A-Za-z][A-Za-z'\-\. ]*[A-Za-z\.]$`)
	// asciiEntryRe 全 ASCII 行的「word pos. 释义」拆分（无中文 / 斜杠 / 制表符可依托）
	asciiEntryRe = regexp.MustCompile(`^([A-Za-z][A-Za-z'\-\. ]*?)\s+((?:(?:n|v|vt|vi|adj|adv|prep|conj|pron|art|num|interj|int|aux|abbr|pl)\.)\s*.+)$`)
	// bracketEntryRe 全 ASCII 行的「word [音标] …」拆分
	bracketEntryRe = regexp.MustCompile(`^([A-Za-z][A-Za-z'\-\. ]*?)\s+(\[[^\[\]]{1,60}\].*)$`)
	// tailPosRe 词头尾部挂着词性的情况（中文切分把 "abandon vt." 整体当词头时）
	tailPosRe = regexp.MustCompile(`^(.+?)\s+((?:n|v|vt|vi|adj|adv|prep|conj|pron|art|num|interj|int|aux|abbr|pl)\.)$`)
)

// parseWordLine 从一行词表文本解析词条，形如：
//
//	apple /ˈæpl/ n. 苹果
//	1. abandon vt. 放弃；抛弃
//	banana	香蕉
//	banana n. a long yellow fruit
//
// 没有释义也没有音标的行默认丢弃（通常是页码 / 标题 / 章节名）。
func parseWordLine(line string, bare bool) (WordEntry, bool) {
	line = strings.TrimSpace(leadingNumRe.ReplaceAllString(line, ""))
	if len(line) < 2 {
		return WordEntry{}, false
	}
	var word, rest string
	if cut := strings.IndexAny(line, "\t/"); cut > 0 {
		word, rest = line[:cut], line[cut:]
	} else if cut := indexHan(line); cut > 0 {
		word, rest = line[:cut], line[cut:]
	} else if m := asciiEntryRe.FindStringSubmatch(line); m != nil {
		word, rest = m[1], m[2]
	} else if m := bracketEntryRe.FindStringSubmatch(line); m != nil {
		word, rest = m[1], m[2]
	} else if bare && wordCharRe.MatchString(line) {
		return WordEntry{Word: strings.Trim(line, " \t.")}, true
	} else {
		return WordEntry{}, false
	}
	word = strings.TrimSpace(word)
	rest = strings.TrimSpace(rest)
	if word == "" || rest == "" {
		return WordEntry{}, false
	}
	// 「abandon vt. 放弃」这类：词性粘在词头尾部，挪回释义一侧
	if m := tailPosRe.FindStringSubmatch(word); m != nil {
		word, rest = m[1], m[2]+" "+rest
	}
	word = strings.Trim(word, " \t·.::;，,")
	if !wordCharRe.MatchString(word) || len(word) > 60 {
		return WordEntry{}, false
	}
	entry := WordEntry{Word: word}
	if m := phoneticRe.FindStringSubmatch(rest); m != nil {
		entry.Phonetic = strings.TrimSpace(m[1])
		rest = strings.TrimSpace(strings.Replace(rest, m[0], " ", 1))
	}
	// 中英混排里词义部分以中文为准，去掉开头的英注残留
	rest = strings.TrimLeft(rest, " \t=：:-—–")
	if m := posRe.FindStringSubmatch(rest); m != nil {
		entry.POS = m[1]
		rest = strings.TrimSpace(rest[len(m[0]):])
	}
	rest = strings.Join(strings.Fields(rest), " ")
	entry.Meaning = rest
	if entry.Meaning == "" && entry.Phonetic == "" && !bare {
		return WordEntry{}, false
	}
	return entry, true
}

// indexHan 第一个 CJK 汉字的位置（没有返回 -1）
func indexHan(s string) int {
	for i, ch := range s {
		if unicode.Is(unicode.Han, ch) {
			return i
		}
	}
	return -1
}

// safeStem 文件名转安全的 JSON 文件名
func safeStem(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case unicode.Is(unicode.Han, r):
			return r
		default:
			return '_'
		}
	}, strings.TrimSpace(s))
	if s == "" {
		s = "book"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}
