package main

// 内置题库：老师出题时可直接导入，省得每道题手敲。
//
// 覆盖：仁爱版七年级英语上册核心词（Unit 1-4）、小学英语基础词、
// 大学英语四级核心词、初中数学公式、语文古诗文默写。
// 英语单词题由词表自动生成选项（同组其它词的中文/英文当干扰项），
// 保证每题 4 个不重复选项，正确答案位置轮换。
//
// 数学/语文题支持 $...$ LaTeX 公式（前端用 KaTeX 渲染）。

import (
	"net/http"
	"strings"
)

// BankQuestion 与 questionReq 字段一致，前端可直接塞进出题表单
type BankQuestion struct {
	Type     string   `json:"type"`
	Stem     string   `json:"stem"`
	Options  []string `json:"options,omitempty"`
	Answer   string   `json:"answer"`
	Score    float64  `json:"score"`
	Analysis string   `json:"analysis,omitempty"`
}

// QuestionBankSet 一个题库集合（一套可直接导入的题）
type QuestionBankSet struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Subject   string         `json:"subject"`
	Kind      string         `json:"kind"`
	Grade     string         `json:"grade"`
	Source    string         `json:"source"`
	Desc      string         `json:"desc"`
	Questions []BankQuestion `json:"questions"`
}

type wordPair struct{ en, zh string }

// buildWordQuestions 用词表生成四选一单词题。dir=zh2en 时题干给中文、选项给英文；
// dir=en2zh 时题干给英文、选项给中文。正确答案按顺序轮换到 A/B/C/D。
func buildWordQuestions(pairs []wordPair, dir string, score float64) []BankQuestion {
	n := len(pairs)
	out := make([]BankQuestion, 0, n)
	for i, p := range pairs {
		opts := make([]string, 4)
		if dir == "zh2en" {
			opts[0] = p.en
			for k := 1; k < 4; k++ {
				opts[k] = pairs[(i+k)%n].en
			}
		} else {
			opts[0] = p.zh
			for k := 1; k < 4; k++ {
				opts[k] = pairs[(i+k)%n].zh
			}
		}
		pos := i % 4
		rotated := make([]string, 4)
		for k := 0; k < 4; k++ {
			rotated[(k+pos)%4] = opts[k]
		}
		stem := "“" + p.zh + "”的英文是？"
		if dir == "en2zh" {
			stem = "“" + p.en + "”的意思是？"
		}
		out = append(out, BankQuestion{
			Type: "choice", Stem: stem, Options: rotated,
			Answer: string(rune('A' + pos)), Score: score,
			Analysis: p.en + " = " + p.zh,
		})
	}
	return out
}

func mcq(stem string, score float64, options []string, answer int, analysis string) BankQuestion {
	return BankQuestion{
		Type: "choice", Stem: stem, Options: options, Score: score,
		Answer: string(rune('A' + answer)), Analysis: analysis,
	}
}

func fillQ(stem string, score float64, answer, analysis string) BankQuestion {
	return BankQuestion{Type: "fill", Stem: stem, Score: score, Answer: answer, Analysis: analysis}
}

// ---------------- 词表 ----------------

// 仁爱版七年级上册 Unit 1-2 核心词（Nice to meet you / Looking different）
var renai7aU12 = []wordPair{
	{"friend", "朋友"}, {"teacher", "老师"}, {"student", "学生"}, {"school", "学校"},
	{"class", "班级"}, {"grade", "年级"}, {"welcome", "欢迎"}, {"meet", "遇见；认识"},
	{"name", "名字"}, {"nice", "好的；友好的"}, {"face", "脸"}, {"eye", "眼睛"},
	{"ear", "耳朵"}, {"nose", "鼻子"}, {"mouth", "嘴"}, {"head", "头"},
	{"hair", "头发"}, {"hand", "手"}, {"foot", "脚"}, {"leg", "腿"},
}

// 仁爱版七年级上册 Unit 3-4 核心词（Getting together / Having fun）
var renai7aU34 = []wordPair{
	{"drink", "喝"}, {"eat", "吃"}, {"food", "食物"}, {"rice", "米饭"},
	{"bread", "面包"}, {"egg", "鸡蛋"}, {"milk", "牛奶"}, {"apple", "苹果"},
	{"banana", "香蕉"}, {"juice", "果汁"}, {"water", "水"}, {"chicken", "鸡肉"},
	{"fish", "鱼"}, {"hamburger", "汉堡包"}, {"cake", "蛋糕"}, {"breakfast", "早餐"},
	{"lunch", "午餐"}, {"dinner", "晚餐"}, {"delicious", "美味的"}, {"hungry", "饥饿的"},
}

// 小学英语基础词（三-六年级常见词）
var primaryWords = []wordPair{
	{"apple", "苹果"}, {"banana", "香蕉"}, {"orange", "橙子"}, {"pear", "梨"},
	{"cat", "猫"}, {"dog", "狗"}, {"bird", "鸟"}, {"fish", "鱼"},
	{"panda", "熊猫"}, {"tiger", "老虎"}, {"monkey", "猴子"}, {"elephant", "大象"},
	{"red", "红色"}, {"blue", "蓝色"}, {"green", "绿色"}, {"yellow", "黄色"},
	{"teacher", "老师"}, {"student", "学生"}, {"doctor", "医生"}, {"nurse", "护士"},
}

// 大学英语四级核心词（高频词表）
var cet4Words = []wordPair{
	{"abandon", "放弃；抛弃"}, {"absorb", "吸收；理解"}, {"academic", "学业的；学术的"}, {"access", "访问；进入"},
	{"accompany", "陪伴；伴随"}, {"accomplish", "完成；实现"}, {"accurate", "准确的"}, {"achieve", "实现；取得"},
	{"acquire", "获得；习得"}, {"adapt", "适应；改编"}, {"adequate", "足够的"}, {"adjust", "调整"},
	{"admire", "钦佩；欣赏"}, {"adopt", "采用；收养"}, {"advance", "前进；进展"}, {"affect", "影响"},
	{"afford", "负担得起"}, {"aggressive", "好斗的；进取的"}, {"alternative", "替代的；选择"}, {"ambitious", "有雄心的"},
}

// ---------------- 题库 ----------------

var questionBankSets = []QuestionBankSet{
	{
		ID: "renai7a-u12", Name: "仁爱版七年级上 Unit 1-2 核心词", Subject: "english", Kind: "word",
		Grade: "初中", Source: "仁爱版（Project English）七年级上册 Unit 1-2",
		Desc:      "nice / friend / face / eye 等 20 个高频词，中文选英文，自动判分",
		Questions: buildWordQuestions(renai7aU12, "zh2en", 2),
	},
	{
		ID: "renai7a-u34", Name: "仁爱版七年级上 Unit 3-4 核心词", Subject: "english", Kind: "word",
		Grade: "初中", Source: "仁爱版（Project English）七年级上册 Unit 3-4",
		Desc:      "eat / drink / food / apple 等 20 个高频词，中文选英文，自动判分",
		Questions: buildWordQuestions(renai7aU34, "zh2en", 2),
	},
	{
		ID: "primary-basic", Name: "小学英语基础词 20 个", Subject: "english", Kind: "word",
		Grade: "小学", Source: "人教版 PEP 三-六年级常见词",
		Desc:      "水果、动物、颜色、职业四类基础词，中文选英文",
		Questions: buildWordQuestions(primaryWords, "en2zh", 2),
	},
	{
		ID: "cet4-core", Name: "大学英语四级核心词 20 个", Subject: "english", Kind: "word",
		Grade: "大学", Source: "四级高频核心词表",
		Desc:      "abandon / access / accomplish 等四级阅读高频词，英文选中文",
		Questions: buildWordQuestions(cet4Words, "en2zh", 2),
	},
	{
		ID: "math-formula", Name: "初中数学公式与计算", Subject: "math", Kind: "quiz",
		Grade: "初中", Source: "初中数学（二次方程 / 勾股定理 / 因式分解）",
		Desc: "题干含 $...$ LaTeX 公式，交卷自动判分",
		Questions: []BankQuestion{
			mcq("一元二次方程 $x^2-5x+6=0$ 的解是？", 4,
				[]string{"$x=2$ 或 $x=3$", "$x=-2$ 或 $x=-3$", "$x=1$ 或 $x=6$", "$x=-1$ 或 $x=-6$"}, 0,
				"因式分解 $(x-2)(x-3)=0$，所以 $x=2$ 或 $x=3$。"),
			mcq("完全平方公式 $(a+b)^2$ 展开后等于？", 4,
				[]string{"$a^2+2ab+b^2$", "$a^2+b^2$", "$a^2-2ab+b^2$", "$a^2-ab+b^2$"}, 0,
				"$(a+b)^2=a^2+2ab+b^2$，注意中间项 $2ab$。"),
			mcq("直角三角形两条直角边分别为 3 和 4，斜边长是？", 3,
				[]string{"5", "$\\sqrt{7}$", "7", "12"}, 0,
				"勾股定理：$3^2+4^2=25$，斜边 $=\\sqrt{25}=5$。"),
			mcq("平方差公式 $a^2-b^2$ 可分解为？", 4,
				[]string{"$(a+b)(a-b)$", "$(a-b)^2$", "$(a+b)^2$", "$a(a-b)$"}, 0,
				"平方差公式：$a^2-b^2=(a+b)(a-b)$。"),
			mcq("一次函数 $y=2x+1$ 的图像经过下面哪个点？", 3,
				[]string{"$(2,5)$", "$(1,1)$", "$(0,2)$", "$(-1,1)$"}, 0,
				"把 $x=2$ 代入得 $y=2\\times2+1=5$，所以过点 $(2,5)$。"),
			mcq("圆面积公式是？", 3,
				[]string{"$S=\\pi r^2$", "$S=2\\pi r$", "$S=\\pi d$", "$S=\\pi r$"}, 0,
				"圆周长 $C=2\\pi r$，面积 $S=\\pi r^2$，别记混。"),
			mcq("二次函数 $y=a(x-h)^2+k$ 的顶点坐标是？", 3,
				[]string{"$(h,k)$", "$(-h,k)$", "$(h,-k)$", "$(-h,-k)$"}, 0,
				"顶点式 $y=a(x-h)^2+k$ 的顶点为 $(h,k)$。"),
			mcq("三角形内角和等于？", 2,
				[]string{"$180^\\circ$", "$90^\\circ$", "$360^\\circ$", "$270^\\circ$"}, 0,
				"任意三角形内角和都是 $180^\\circ$。"),
			fillQ("计算 $\\frac{1}{2}+\\frac{1}{3}=$ ____（填最简分数，如 5/6）", 3, "5/6|五分之六",
				"通分：$\\frac{3}{6}+\\frac{2}{6}=\\frac{5}{6}$。"),
			fillQ("分解因式：$x^2-4=$ ____", 4, "(x+2)(x-2)|(x-2)(x+2)",
				"平方差公式：$x^2-4=(x+2)(x-2)$。"),
		},
	},
	{
		ID: "chinese-poem", Name: "语文古诗文默写 10 题", Subject: "chinese", Kind: "quiz",
		Grade: "初中", Source: "部编版初中语文必背古诗文",
		Desc: "上句接下句，填空自动判分；标点不用写",
		Questions: []BankQuestion{
			fillQ("床前明月光，____。", 4, "疑是地上霜", "出自李白《静夜思》。"),
			fillQ("白日依山尽，____。", 4, "黄河入海流", "出自王之涣《登鹳雀楼》。"),
			fillQ("春眠不觉晓，____。", 4, "处处闻啼鸟", "出自孟浩然《春晓》。"),
			fillQ("海内存知己，____。", 4, "天涯若比邻", "出自王勃《送杜少府之任蜀州》。"),
			fillQ("会当凌绝顶，____。", 4, "一览众山小", "出自杜甫《望岳》。"),
			fillQ("野火烧不尽，____。", 4, "春风吹又生", "出自白居易《赋得古原草送别》。"),
			fillQ("人生自古谁无死？____。", 4, "留取丹心照汗青", "出自文天祥《过零丁洋》。"),
			fillQ("沉舟侧畔千帆过，____。", 4, "病树前头万木春", "出自刘禹锡《酬乐天扬州初逢席上见赠》。"),
			mcq("“但愿人长久，千里共婵娟”出自哪位词人的作品？", 3,
				[]string{"苏轼", "辛弃疾", "李清照", "柳永"}, 0, "出自苏轼《水调歌头·明月几时有》。"),
			mcq("《静夜思》的作者是？", 2, []string{"李白", "杜甫", "王维", "白居易"}, 0,
				"《静夜思》是李白的代表作，写思乡之情。"),
		},
	},
}

// handleQuestionBank 返回内置题库集合（老师 / 管理员出题时导入用）
func (a *API) handleQuestionBank(w http.ResponseWriter, r *http.Request, u *User) {
	q := strings.TrimSpace(r.URL.Query().Get("subject"))
	out := make([]QuestionBankSet, 0, len(questionBankSets))
	for _, set := range questionBankSets {
		if q != "" && set.Subject != q {
			continue
		}
		out = append(out, set)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sets": out})
}
