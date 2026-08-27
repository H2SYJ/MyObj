package tagging

import (
	"testing"

	"myobj/src/pkg/models"
)

func testSnapshot(t *testing.T, extraRules ...models.TagRule) *Snapshot {
	t.Helper()
	global := models.TagRuleSet{
		Version: 3,
		Rules: []models.TagRule{
			{ID: "year", Type: models.TagRuleTypeRegex, TargetField: "basename", Pattern: `((?:19|20)\d{2})`, Replacement: "$1", CategoryID: "year", Priority: 90, Weight: 1, Enabled: true},
			{ID: "resolution", Type: models.TagRuleTypeRegex, TargetField: "basename", Pattern: `(?i)(2160p|4k|1080p)`, Replacement: "$1", CategoryID: "resolution", Priority: 100, Weight: 1, Enabled: true},
			{ID: "codec", Type: models.TagRuleTypeRegex, TargetField: "basename", Pattern: `(?i)(h\.?265|hevc)`, Replacement: "$1", CategoryID: "codec", Priority: 95, Weight: 1, Enabled: true},
			{ID: "forced", Type: models.TagRuleTypeWord, Pattern: "流浪地球", CategoryID: "title", Priority: 80, Weight: 1, Enabled: true},
			{ID: "stop", Type: models.TagRuleTypeStopWord, Pattern: "完整版", Enabled: true},
		},
	}
	global.Rules = append(global.Rules, extraRules...)
	snapshot, err := CompileSnapshot(global, 20)
	if err != nil {
		t.Fatalf("编译规则失败: %v", err)
	}
	return snapshot
}

func TestGenerateFilenameAndMetadataTags(t *testing.T) {
	snapshot := testSnapshot(t)
	tags := snapshot.Generate(Input{
		Filename: "流浪地球2.2023.2160p.WEB-DL.H.265.完整版.mkv",
		MIME:     "video/x-matroska", Size: 2 * 1024 * 1024 * 1024,
	})
	assertTag(t, tags, "流浪地球", "title")
	assertNoTag(t, tags, "2023")
	assertTag(t, tags, "4K", "resolution")
	assertTag(t, tags, "H265", "codec")
	assertTag(t, tags, "视频", "file_type")
	assertTag(t, tags, "MKV", "file_type")
	assertNoTag(t, tags, "完整版")
}

func TestGlobalAliasAndCustomWordApplyToGenerationAndQuery(t *testing.T) {
	snapshot := testSnapshot(t,
		models.TagRule{ID: "global-word", Type: models.TagRuleTypeWord, Pattern: "葬送的芙莉莲", CategoryID: "title", Priority: 10, Weight: 1, Enabled: true},
		models.TagRule{ID: "global-alias", Type: models.TagRuleTypeAlias, Pattern: "hevc", Replacement: "高效视频编码", CategoryID: "codec", Priority: 10, Weight: 1, Enabled: true},
	)
	if snapshot.GlobalVersion != 3 {
		t.Fatalf("规则版本错误: global=%d", snapshot.GlobalVersion)
	}
	tags := snapshot.Generate(Input{Filename: "葬送的芙莉莲.hevc.mkv", MIME: "video/x-matroska"})
	assertTag(t, tags, "葬送的芙莉莲", "title")
	query := snapshot.TokenizeQuery("葬送的芙莉莲 hevc")
	if len(query) == 0 {
		t.Fatal("查询分词不应为空")
	}
}

func TestValidTagNameRejectsBOMAndControlCharacters(t *testing.T) {
	for _, value := range []string{string(rune(0xFEFF)) + "标签", "标\n签", ""} {
		if ValidTagName(value) {
			t.Fatalf("应拒绝非法标签%q", value)
		}
	}
	if !ValidTagName("中文标签") {
		t.Fatal("应接受中文标签")
	}
}

func TestLongestForcedWordWinsAndPriorityResolvesSamePhrase(t *testing.T) {
	global := models.TagRuleSet{Version: 1, Rules: []models.TagRule{
		{ID: "global-long", Type: models.TagRuleTypeWord, Pattern: "人工智能", CategoryID: "title", Priority: 1, Weight: 1, Enabled: true},
		{ID: "global-short", Type: models.TagRuleTypeWord, Pattern: "智能", CategoryID: "other", Priority: 999, Weight: 1, Enabled: true},
		{ID: "global-same", Type: models.TagRuleTypeWord, Pattern: "大模型", CategoryID: "other", Priority: 1, Weight: 1, Enabled: true},
		{ID: "priority-same", Type: models.TagRuleTypeWord, Pattern: "大模型", CategoryID: "title", Priority: 999, Weight: 1, Enabled: true},
	}}
	snapshot, err := CompileSnapshot(global, 20)
	if err != nil {
		t.Fatal(err)
	}
	tags := snapshot.Generate(Input{Filename: "人工智能与大模型.txt", MIME: "text/plain"})
	assertRuleTag(t, tags, "人工智能", "global-long")
	assertNoTag(t, tags, "智能")
	assertRuleTag(t, tags, "大模型", "priority-same")
}

func TestNFKCAliasTieBreakStopWordUnionAndLimit(t *testing.T) {
	rules := []models.TagRule{
		{ID: "word", Type: models.TagRuleTypeWord, Pattern: "ＡＩ", CategoryID: "title", Priority: 10, Weight: 1, Enabled: true},
		{ID: "alias-z", Type: models.TagRuleTypeAlias, Pattern: "hevc", Replacement: "较后别名", CategoryID: "codec", Priority: 10, Enabled: true},
		{ID: "alias-a", Type: models.TagRuleTypeAlias, Pattern: "hevc", Replacement: "稳定别名", CategoryID: "codec", Priority: 10, Enabled: true},
		{ID: "stop-global", Type: models.TagRuleTypeStopWord, Pattern: "完整版", Enabled: true},
		{ID: "regex", Type: models.TagRuleTypeRegex, TargetField: "basename", Pattern: `(?i)(hevc)`, Replacement: "$1", CategoryID: "codec", Priority: 50, Weight: 1, Enabled: true},
	}
	rules = append(rules, models.TagRule{ID: "stop-extra", Type: models.TagRuleTypeStopWord, Pattern: "电影", Enabled: true})
	global := models.TagRuleSet{Version: 1, Rules: rules}
	snapshot, err := CompileSnapshot(global, 20)
	if err != nil {
		t.Fatal(err)
	}
	tags := snapshot.Generate(Input{Filename: "ＡＩ.hevc.完整版.电影.mkv", MIME: "video/x-matroska"})
	assertTag(t, tags, "AI", "title")
	assertTag(t, tags, "稳定别名", "codec")
	assertNoTag(t, tags, "完整版")
	assertNoTag(t, tags, "电影")

	limited, err := CompileSnapshot(global, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(limited.Generate(Input{Filename: "ＡＩ.hevc.mkv", MIME: "video/x-matroska"})); got != 1 {
		t.Fatalf("自动标签数量限制无效: %d", got)
	}
}

func TestNFKCIsAppliedBeforeExtensionAndRegexExtraction(t *testing.T) {
	snapshot := testSnapshot(t)
	tags := snapshot.Generate(Input{Filename: "电影．２０２３．ｍｋｖ", MIME: "video/x-matroska"})
	assertNoTag(t, tags, "2023")
	assertTag(t, tags, "MKV", "file_type")
}

func TestPreprocessTitleSplitsBoundariesAndReplacesPunctuation(t *testing.T) {
	actual := preprocessTitle("ＭｙHTTPServer2-最终版（副本）")
	if actual != "my http server 2 最终版 副本" {
		t.Fatalf("标题预处理结果错误: %q", actual)
	}
}

func TestStrictFilenameTokenFiltering(t *testing.T) {
	tests := []struct {
		name  string
		token SegmentToken
		want  bool
	}{
		{name: "允许中文名词", token: SegmentToken{Text: "人工智能", POS: "n"}, want: true},
		{name: "拒绝中文形容词", token: SegmentToken{Text: "美丽", POS: "a"}},
		{name: "允许纯字母词", token: SegmentToken{Text: "AI", POS: "x"}, want: true},
		{name: "拒绝字母数字噪声", token: SegmentToken{Text: "A1", POS: "n"}},
		{name: "拒绝纯数字", token: SegmentToken{Text: "2024", POS: "m"}},
		{name: "拒绝单字符", token: SegmentToken{Text: "A", POS: "x"}},
		{name: "拒绝常见中文文件名噪声", token: SegmentToken{Text: "文件", POS: "n"}},
		{name: "拒绝常见英文文件名噪声", token: SegmentToken{Text: "final", POS: "x"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := validFilenameToken(test.token, nil, nil); actual != test.want {
				t.Fatalf("过滤结果错误: got=%v want=%v token=%+v", actual, test.want, test.token)
			}
		})
	}
}

func TestGenerateFiltersPureNumericTagsFromEveryAutomaticSource(t *testing.T) {
	global := models.TagRuleSet{Version: 1, Rules: []models.TagRule{
		{ID: "word", Type: models.TagRuleTypeWord, Pattern: "１２３", CategoryID: "title", Enabled: true},
		{ID: "regex", Type: models.TagRuleTypeRegex, TargetField: "basename", Pattern: `(2024)`, Replacement: "$1", CategoryID: "year", Enabled: true},
	}}
	snapshot, err := CompileSnapshot(global, 20)
	if err != nil {
		t.Fatal(err)
	}
	tags := snapshot.Generate(Input{Filename: "１２３.2024.mkv", Metadata: map[string]string{"year": "２０２５"}})
	for _, name := range []string{"123", "2024", "2025"} {
		assertNoTag(t, tags, name)
	}
}

func TestIsPureNumericTagNameUsesNFKCAndUnicodeDigits(t *testing.T) {
	for _, value := range []string{"123", "１２３", "١٢٣"} {
		if !IsPureNumericTagName(value) {
			t.Fatalf("应识别纯数字标签%q", value)
		}
	}
	for _, value := range []string{"", "2024年", "S01E02", "1.5"} {
		if IsPureNumericTagName(value) {
			t.Fatalf("不应识别为纯数字标签%q", value)
		}
	}
}

func TestInvalidUTF8FilenameKeepsOnlyBasicMetadataTags(t *testing.T) {
	snapshot := testSnapshot(t)
	tags := snapshot.Generate(Input{Filename: string([]byte{0xff, '.', 'm', 'k', 'v'}), MIME: "video/x-matroska"})
	assertTag(t, tags, "视频", "file_type")
	assertNoTag(t, tags, "MKV")
	for _, tag := range tags {
		if tag.SourceType == models.TagSourceFilename || tag.SourceType == models.TagSourceRule {
			t.Fatalf("非法 UTF-8 文件名不应生成文件名或规则标签: %#v", tag)
		}
	}
}

func TestQueryAppliesAliasAndStopWordsToForcedTerms(t *testing.T) {
	global := models.TagRuleSet{Version: 1, Rules: []models.TagRule{
		{ID: "word-a", Type: models.TagRuleTypeWord, Pattern: "人工智能", CategoryID: "title", Enabled: true},
		{ID: "word-b", Type: models.TagRuleTypeWord, Pattern: "机器学习", CategoryID: "title", Enabled: true},
		{ID: "stop", Type: models.TagRuleTypeStopWord, Pattern: "人工智能", Enabled: true},
		{ID: "alias", Type: models.TagRuleTypeAlias, Pattern: "机器学习", Replacement: "ML", CategoryID: "title", Enabled: true},
	}}
	snapshot, err := CompileSnapshot(global, 20)
	if err != nil {
		t.Fatal(err)
	}
	terms := snapshot.TokenizeQuery("人工智能 机器学习")
	if len(terms) != 1 || terms[0] != "ML" {
		t.Fatalf("查询强制词没有应用停用词和别名: %#v", terms)
	}
}

func assertTag(t *testing.T, tags []Candidate, name, category string) {
	t.Helper()
	for _, tag := range tags {
		if tag.Name == name && tag.CategoryID == category {
			return
		}
	}
	t.Fatalf("未找到标签%s/%s: %#v", name, category, tags)
}

func assertNoTag(t *testing.T, tags []Candidate, name string) {
	t.Helper()
	for _, tag := range tags {
		if tag.Name == name {
			t.Fatalf("不应包含标签%s: %#v", name, tags)
		}
	}
}

func assertRuleTag(t *testing.T, tags []Candidate, name, sourceKey string) {
	t.Helper()
	for _, tag := range tags {
		if tag.Name == name && tag.SourceType == models.TagSourceRule && tag.SourceKey == sourceKey {
			return
		}
	}
	t.Fatalf("未找到规则标签%s/%s: %#v", name, sourceKey, tags)
}

func assertNoRuleTag(t *testing.T, tags []Candidate, name, sourceKey string) {
	t.Helper()
	for _, tag := range tags {
		if tag.Name == name && tag.SourceType == models.TagSourceRule && tag.SourceKey == sourceKey {
			t.Fatalf("不应包含规则标签%s/%s: %#v", name, sourceKey, tags)
		}
	}
}
