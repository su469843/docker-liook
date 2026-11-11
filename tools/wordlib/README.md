# wordlib：词书 PDF → JSON 词库

把词书 PDF 末尾几页的单词表抽成 JSON，供后续「词书 / 打卡」功能使用。
**当前阶段只落盘、不接入服务端**：输出写到仓库根目录的 `vocab/`（已在 .gitignore 里，
因为词书内容有版权，不要提交到公开仓库）。

## 用法

```bash
# 本地目录（递归找 PDF）
go run ./tools/wordlib -in D:\词书PDF -out vocab

# 单个 PDF
go run ./tools/wordlib -in 单词书.pdf -last 10

# git 仓库（确定仓库地址后填这里，浅克隆到临时目录再抽）
go run ./tools/wordlib -in https://github.com/<用户>/<词书仓库>.git -out vocab
```

参数：

| 参数 | 默认 | 说明 |
|---|---|---|
| `-in` | 必填 | PDF 文件 / 目录 / git 仓库地址 |
| `-sub` | 空 | 只处理路径含该关键字的 PDF |
| `-last` | 10 | 每本只解析末尾 N 页（词表一般在书末附录），0 = 全书 |
| `-out` | vocab | JSON 输出目录 |
| `-bare` | false | 保留没有释义的纯单词行 |

## 输出格式

每本 PDF 一个 `vocab/<书名>.json`：

```json
{
  "book": "初中词汇手册",
  "source": "https://github.com/...git",
  "pages": "180-190/190",
  "count": 1620,
  "generated_at": "2026-11-01T20:00:00+08:00",
  "words": [
    { "word": "apple", "phonetic": "ˈæpl", "pos": "n.", "meaning": "苹果" },
    { "word": "abandon", "pos": "vt.", "meaning": "放弃；抛弃" }
  ]
}
```

外加一个 `vocab/index.json` 汇总所有书。

## 已知限制

- 只支持**文本型** PDF；扫描图片版 PDF 抽不到字（需要 OCR，后续再说）。
- 行解析按「词头 + 音标 + 词性 + 释义」的常见排版写，排版特殊的书可能漏行，
  抽完看一眼 count 是否合理。
- 词库仅供本站教学自用，请勿把抽取结果再分发。
