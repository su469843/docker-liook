package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseWordLine(t *testing.T) {
	cases := []struct {
		line   string
		want   WordEntry
		wantOK bool
		bare   bool
	}{
		{line: "apple /ˈæpl/ n. 苹果", want: WordEntry{Word: "apple", Phonetic: "ˈæpl", POS: "n.", Meaning: "苹果"}, wantOK: true},
		{line: "1. abandon vt. 放弃；抛弃", want: WordEntry{Word: "abandon", POS: "vt.", Meaning: "放弃；抛弃"}, wantOK: true},
		{line: "banana\t香蕉", want: WordEntry{Word: "banana", Meaning: "香蕉"}, wantOK: true},
		{line: "give up  放弃", want: WordEntry{Word: "give up", Meaning: "放弃"}, wantOK: true},
		{line: "word list", wantOK: false},
		{line: "12", wantOK: false},
		{line: "", wantOK: false},
		{line: "Unit 1", wantOK: false},
		// 纯单词行：默认丢，bare 时保留
		{line: "hello", wantOK: false},
		{line: "hello", wantOK: true, bare: true, want: WordEntry{Word: "hello"}},
		// 只有音标没有释义也保留
		{line: "cat [kæt]", want: WordEntry{Word: "cat", Phonetic: "kæt"}, wantOK: true},
	}
	for _, c := range cases {
		got, ok := parseWordLine(c.line, c.bare)
		if ok != c.wantOK {
			t.Errorf("parseWordLine(%q, bare=%v) ok=%v, want %v", c.line, c.bare, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if got != c.want {
			t.Errorf("parseWordLine(%q) = %+v, want %+v", c.line, got, c.want)
		}
	}
}

// buildTestPDF 手工拼一个最小 PDF（WinAnsi 文本，每行一个 Tj），返回字节与正确的 xref
func buildTestPDF(t *testing.T, lines []string) []byte {
	t.Helper()
	var content strings.Builder
	content.WriteString("BT /F1 12 Tf 72 720 Td\n")
	for _, l := range lines {
		esc := strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(l)
		content.WriteString("(" + esc + ") Tj T*\n")
	}
	content.WriteString("ET")
	stream := content.String()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Length " + itoa(len(stream)) + " >>\nstream\n" + stream + "\nendstream",
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		b.WriteString(itoa(i+1) + " 0 obj\n" + o + "\nendobj\n")
	}
	xref := b.Len()
	b.WriteString("xref\n0 " + itoa(len(objs)+1) + "\n0000000000 65535 f \n")
	for _, off := range offsets {
		b.WriteString(pad10(off) + " 00000 n \n")
	}
	b.WriteString("trailer\n<< /Size " + itoa(len(objs)+1) + " /Root 1 0 R >>\nstartxref\n" + itoa(xref) + "\n%%EOF\n")
	return []byte(b.String())
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func pad10(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 10 {
		s = "0" + s
	}
	return s
}

func TestExtractBookFromPDF(t *testing.T) {
	// 手拼的 PDF 用 WinAnsi 编码，塞不进原生 UTF-8 中文；
	// 中文切分/释义逻辑已在 TestParseWordLine 覆盖，这里验证 PDF 抽取主流程
	lines := []string{
		"Word List",
		"1. apple /ae-pl/ n. a round fruit",
		"2. banana n. a long yellow fruit",
		"3. cat [k-at] n. a small animal",
		"Page 1",
	}
	pdfBytes := buildTestPDF(t, lines)
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.pdf")
	if err := os.WriteFile(path, pdfBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	book, err := extractBook(path, "test-source", 10, false, "2026-11-01T00:00:00+08:00")
	if err != nil {
		t.Fatalf("extractBook: %v", err)
	}
	if book.Count == 0 {
		t.Fatalf("没抽到词条，Pages=%s", book.Pages)
	}
	got := map[string]WordEntry{}
	for _, e := range book.Entries {
		got[strings.ToLower(e.Word)] = e
	}
	if e, ok := got["apple"]; !ok || e.POS != "n." || e.Meaning != "a round fruit" {
		t.Errorf("apple 解析异常: %+v (all=%+v)", e, book.Entries)
	}
	if e, ok := got["banana"]; !ok || e.POS != "n." || e.Meaning != "a long yellow fruit" {
		t.Errorf("banana 解析异常: %+v (all=%+v)", e, book.Entries)
	}
	if e, ok := got["cat"]; !ok || e.Phonetic != "k-at" {
		t.Errorf("cat 解析异常: %+v", e)
	}
	if _, ok := got["word"]; ok {
		t.Errorf("标题行不应被收录: %+v", book.Entries)
	}
	if _, ok := got["page"]; ok {
		t.Errorf("页码行不应被收录: %+v", book.Entries)
	}
}
