package tagging

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-ego/gse"
	"golang.org/x/text/unicode/norm"

	"myobj/src/pkg/models"
)

const (
	DefaultAutoTagLimit = 20
	MaxTagRunes         = 64
)

var separatorPattern = regexp.MustCompile(`[\s._\-\[\](){}【】（）]+`)

var (
	acronymBoundaryPattern    = regexp.MustCompile(`([\p{Lu}]+)([\p{Lu}][\p{Ll}])`)
	lowerUpperBoundaryPattern = regexp.MustCompile(`([\p{Ll}\p{N}])([\p{Lu}])`)
	letterDigitPattern        = regexp.MustCompile(`([\p{L}])([\p{N}])`)
	digitLetterPattern        = regexp.MustCompile(`([\p{N}])([\p{L}])`)
)

var allowedFilenamePOS = map[string]struct{}{
	"n": {}, "v": {}, "vn": {}, "nr": {}, "ns": {}, "nt": {}, "nz": {},
}

var defaultFilenameStopWords = map[string]struct{}{
	"一个": {}, "进行": {}, "相关": {}, "文件": {}, "文档": {}, "新建": {}, "未命名": {},
	"最终": {}, "最终版": {}, "副本": {}, "草稿": {}, "备份": {}, "扫描": {}, "下载": {},
	"copy": {}, "draft": {}, "final": {}, "backup": {}, "scan": {}, "temp": {}, "tmp": {},
}

// Tokenizer 隔离具体分词实现，便于后续替换或增加语言模型。
type Tokenizer interface {
	Segment(text string) []string
	SegmentWithPOS(text string) []SegmentToken
	IsStopWord(text string) bool
}

type SegmentToken struct {
	Text string
	POS  string
}

type GSETokenizer struct {
	segmenter *gse.Segmenter
}

func (t *GSETokenizer) Segment(text string) []string {
	if t == nil || t.segmenter == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	return t.segmenter.Cut(text, true)
}

func (t *GSETokenizer) SegmentWithPOS(text string) []SegmentToken {
	if t == nil || t.segmenter == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	pairs := t.segmenter.Pos(text, false)
	result := make([]SegmentToken, 0, len(pairs))
	for _, pair := range pairs {
		result = append(result, SegmentToken{Text: pair.Text, POS: pair.Pos})
	}
	return result
}

func (t *GSETokenizer) IsStopWord(text string) bool {
	return t != nil && t.segmenter != nil && t.segmenter.IsStop(text)
}

type Input struct {
	Filename string
	MIME     string
	Size     int64
	IsEnc    bool
	Metadata map[string]string
}

type Candidate struct {
	Name       string  `json:"name"`
	Normalized string  `json:"normalized"`
	CategoryID string  `json:"category_id"`
	SourceType string  `json:"source_type"`
	SourceKey  string  `json:"source_key"`
	Priority   int     `json:"priority"`
	Weight     float64 `json:"weight"`
}

type compiledRegexRule struct {
	id          string
	targetField string
	replacement string
	categoryID  string
	priority    int
	weight      float64
	expression  *regexp.Regexp
}

type aliasTarget struct {
	id         string
	name       string
	categoryID string
	priority   int
}

type forcedTerm struct {
	id         string
	name       string
	normalized string
	categoryID string
	priority   int
	weight     float64
}

type phraseNode struct {
	children map[rune]*phraseNode
	terms    []forcedTerm
}

type phraseMatcher struct {
	root *phraseNode
}

func newPhraseMatcher(terms []forcedTerm) *phraseMatcher {
	matcher := &phraseMatcher{root: &phraseNode{children: map[rune]*phraseNode{}}}
	for _, term := range terms {
		node := matcher.root
		for _, char := range []rune(term.normalized) {
			if node.children[char] == nil {
				node.children[char] = &phraseNode{children: map[rune]*phraseNode{}}
			}
			node = node.children[char]
		}
		node.terms = append(node.terms, term)
	}
	return matcher
}

func (m *phraseMatcher) find(text string) []forcedTerm {
	terms, _ := m.protect(text)
	return terms
}

// protect 返回按最长匹配选中的强制词，并用空格掩码其原始范围，避免后续分词再次拆出子词。
func (m *phraseMatcher) protect(text string) ([]forcedTerm, string) {
	if m == nil || m.root == nil {
		return nil, text
	}
	runes := []rune(text)
	remaining := append([]rune(nil), runes...)
	found := make(map[string]forcedTerm)
	for start := 0; start < len(runes); start++ {
		node := m.root
		var best *forcedTerm
		bestLength := 0
		bestEnd := start
		for index := start; index < len(runes); index++ {
			node = node.children[runes[index]]
			if node == nil {
				break
			}
			for _, term := range node.terms {
				length := index - start + 1
				if best == nil || length > bestLength ||
					(length == bestLength && (term.priority > best.priority ||
						(term.priority == best.priority && term.id < best.id))) {
					candidate := term
					best = &candidate
					bestLength = length
					bestEnd = index
				}
			}
		}
		if best != nil {
			key := best.normalized + "\x00" + best.categoryID
			current, exists := found[key]
			if !exists || best.priority > current.priority ||
				(best.priority == current.priority && best.id < current.id) {
				found[key] = *best
			}
			for index := start; index <= bestEnd; index++ {
				remaining[index] = ' '
			}
			start = bestEnd
		}
	}
	result := make([]forcedTerm, 0, len(found))
	for _, term := range found {
		result = append(result, term)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].priority != result[j].priority {
			return result[i].priority > result[j].priority
		}
		if len([]rune(result[i].name)) != len([]rune(result[j].name)) {
			return len([]rune(result[i].name)) > len([]rune(result[j].name))
		}
		return result[i].id < result[j].id
	})
	return result, string(remaining)
}

// Snapshot 是可并发只读的完整规则快照。
type Snapshot struct {
	GlobalVersion int64
	Limit         int
	tokenizer     Tokenizer
	regexRules    []compiledRegexRule
	aliases       map[string]aliasTarget
	stopWords     map[string]struct{}
	matcher       *phraseMatcher
}

func CompileSnapshot(ruleSet models.TagRuleSet, limit int) (*Snapshot, error) {
	if limit < 1 || limit > 100 {
		limit = DefaultAutoTagLimit
	}
	snapshot := &Snapshot{
		GlobalVersion: ruleSet.Version, Limit: limit,
		aliases: map[string]aliasTarget{}, stopWords: map[string]struct{}{},
	}
	forced := make([]forcedTerm, 0)
	for _, rule := range ruleSet.Rules {
		if !rule.Enabled {
			continue
		}
		priority := rule.Priority
		categoryID := rule.CategoryID
		if categoryID == "" {
			categoryID = "other"
		}
		switch rule.Type {
		case models.TagRuleTypeWord:
			name := displayTagName(rule.Pattern)
			normalized := Normalize(name)
			if normalized == "" {
				return nil, fmt.Errorf("自定义词不能为空: %s", rule.ID)
			}
			forced = append(forced, forcedTerm{
				id: rule.ID, name: name, normalized: normalized, categoryID: categoryID,
				priority: 50000 + priority, weight: rule.Weight,
			})
		case models.TagRuleTypeStopWord:
			word := Normalize(rule.Pattern)
			if word != "" {
				snapshot.stopWords[word] = struct{}{}
			}
		case models.TagRuleTypeAlias:
			pattern, replacement := Normalize(rule.Pattern), displayTagName(rule.Replacement)
			if pattern == "" || replacement == "" {
				return nil, fmt.Errorf("别名规则必须包含原词和目标词: %s", rule.ID)
			}
			current, exists := snapshot.aliases[pattern]
			if !exists || priority > current.priority || (priority == current.priority && rule.ID < current.id) {
				snapshot.aliases[pattern] = aliasTarget{id: rule.ID, name: replacement, categoryID: categoryID, priority: priority}
			}
		case models.TagRuleTypeRegex:
			if len(rule.Pattern) > 512 {
				return nil, fmt.Errorf("正则规则超过512字符: %s", rule.ID)
			}
			expression, err := regexp.Compile(rule.Pattern)
			if err != nil {
				return nil, fmt.Errorf("正则规则%s无效: %w", rule.ID, err)
			}
			snapshot.regexRules = append(snapshot.regexRules, compiledRegexRule{
				id: rule.ID, targetField: rule.TargetField, replacement: rule.Replacement,
				categoryID: categoryID, priority: priority, weight: rule.Weight, expression: expression,
			})
		default:
			return nil, fmt.Errorf("不支持的标签规则类型: %s", rule.Type)
		}
	}
	tokenizer, err := sharedGSETokenizer()
	if err != nil {
		return nil, err
	}
	snapshot.tokenizer = tokenizer
	snapshot.matcher = newPhraseMatcher(forced)
	sort.SliceStable(snapshot.regexRules, func(i, j int) bool {
		if snapshot.regexRules[i].priority != snapshot.regexRules[j].priority {
			return snapshot.regexRules[i].priority > snapshot.regexRules[j].priority
		}
		return snapshot.regexRules[i].id < snapshot.regexRules[j].id
	})
	return snapshot, nil
}

func (s *Snapshot) Generate(input Input) []Candidate {
	if s == nil {
		return nil
	}
	filename := strings.TrimSpace(input.Filename)
	if !utf8.ValidString(filename) {
		filename = ""
	} else {
		filename = displayTagName(filename)
	}
	extension := strings.TrimPrefix(filepath.Ext(filename), ".")
	basename := strings.TrimSuffix(filename, filepath.Ext(filename))
	normalizedBase := preprocessTitle(basename)
	normalizedInput := input
	normalizedInput.Filename = Normalize(filename)
	normalizedInput.MIME = Normalize(input.MIME)
	if len(input.Metadata) > 0 {
		normalizedInput.Metadata = make(map[string]string, len(input.Metadata))
		for key, value := range input.Metadata {
			normalizedInput.Metadata[key] = Normalize(value)
		}
	}
	candidates := make([]Candidate, 0, s.Limit*2)

	for _, rule := range s.regexRules {
		target := ruleTarget(normalizedInput, Normalize(basename), Normalize(extension), rule.targetField)
		for _, indexes := range rule.expression.FindAllStringSubmatchIndex(target, -1) {
			value := rule.expression.ExpandString(nil, rule.replacement, target, indexes)
			if len(value) == 0 && len(indexes) >= 2 {
				value = []byte(target[indexes[0]:indexes[1]])
			}
			candidates = append(candidates, Candidate{
				Name: string(value), CategoryID: rule.categoryID, SourceType: models.TagSourceRule,
				SourceKey: rule.id, Priority: rule.priority, Weight: rule.weight,
			})
		}
	}

	forcedTerms, remainingBase := s.matcher.protect(normalizedBase)
	for _, term := range forcedTerms {
		candidates = append(candidates, Candidate{
			Name: term.name, CategoryID: term.categoryID, SourceType: models.TagSourceRule,
			SourceKey: term.id, Priority: term.priority, Weight: term.weight,
		})
	}

	for _, token := range s.tokenizer.SegmentWithPOS(remainingBase) {
		if validFilenameToken(token, s.tokenizer, s.stopWords) {
			candidates = append(candidates, Candidate{
				Name: token.Text, CategoryID: "title", SourceType: models.TagSourceFilename,
				SourceKey: "gse", Priority: 20, Weight: 1,
			})
		}
	}

	candidates = append(candidates, basicMetadataCandidates(input, extension)...)
	for key, value := range input.Metadata {
		if strings.TrimSpace(value) == "" {
			continue
		}
		categoryID := metadataCategory(key)
		if categoryID != "" {
			candidates = append(candidates, Candidate{
				Name: value, CategoryID: categoryID, SourceType: models.TagSourceMetadata,
				SourceKey: key, Priority: 60, Weight: 1,
			})
		}
	}
	return s.finalize(candidates)
}

func (s *Snapshot) TokenizeQuery(query string) []string {
	query = Normalize(separatorPattern.ReplaceAllString(query, " "))
	result := make([]string, 0)
	seen := map[string]struct{}{}
	forcedTerms, remainingQuery := s.matcher.protect(query)
	for _, term := range forcedTerms {
		normalized := term.normalized
		name := term.name
		if alias, exists := s.aliases[normalized]; exists {
			normalized = Normalize(alias.name)
			name = alias.name
		}
		if _, stopped := s.stopWords[normalized]; stopped {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, displayTagName(name))
	}
	for _, word := range s.tokenizer.Segment(remainingQuery) {
		normalized := Normalize(word)
		if !validSegment(normalized) {
			continue
		}
		if alias, exists := s.aliases[normalized]; exists {
			normalized = Normalize(alias.name)
			word = alias.name
		}
		if _, stopped := s.stopWords[normalized]; stopped {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, displayTagName(word))
	}
	return result
}

func (s *Snapshot) finalize(input []Candidate) []Candidate {
	result := make([]Candidate, 0, min(len(input), s.Limit))
	deduplicated := map[string]Candidate{}
	for _, candidate := range input {
		candidate.Name = normalizeKnownTag(displayTagName(candidate.Name), candidate.CategoryID)
		candidate.Normalized = Normalize(candidate.Name)
		if alias, exists := s.aliases[candidate.Normalized]; exists {
			candidate.Name = alias.name
			candidate.Normalized = Normalize(alias.name)
			if alias.categoryID != "" && alias.categoryID != "other" {
				candidate.CategoryID = alias.categoryID
			}
		}
		if !ValidTagName(candidate.Name) || IsPureNumericTagName(candidate.Name) {
			continue
		}
		if _, stopped := s.stopWords[candidate.Normalized]; stopped {
			continue
		}
		if candidate.CategoryID == "" {
			candidate.CategoryID = "other"
		}
		key := candidate.Normalized + "\x00" + candidate.CategoryID
		current, exists := deduplicated[key]
		if !exists || candidate.Priority > current.Priority ||
			(candidate.Priority == current.Priority && candidate.Weight > current.Weight) {
			deduplicated[key] = candidate
		}
	}
	for _, candidate := range deduplicated {
		result = append(result, candidate)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		if result[i].Weight != result[j].Weight {
			return result[i].Weight > result[j].Weight
		}
		return result[i].Normalized < result[j].Normalized
	})
	if len(result) > s.Limit {
		result = result[:s.Limit]
	}
	return result
}

func ruleTarget(input Input, basename, extension, field string) string {
	switch field {
	case "filename":
		return input.Filename
	case "extension":
		return extension
	case "mime":
		return input.MIME
	case "basename", "":
		return basename
	default:
		if strings.HasPrefix(field, "metadata.") {
			return input.Metadata[strings.TrimPrefix(field, "metadata.")]
		}
		return ""
	}
}

func basicMetadataCandidates(input Input, extension string) []Candidate {
	result := make([]Candidate, 0, 4)
	if extension != "" && len([]rune(extension)) <= 12 {
		result = append(result, Candidate{Name: strings.ToUpper(extension), CategoryID: "file_type", SourceType: models.TagSourceMetadata, SourceKey: "extension", Priority: 80, Weight: 1})
	}
	mainType := strings.ToLower(strings.SplitN(input.MIME, "/", 2)[0])
	typeName := map[string]string{"image": "图片", "video": "视频", "audio": "音频", "text": "文档"}[mainType]
	if typeName == "" && input.MIME != "" {
		if strings.Contains(input.MIME, "zip") || strings.Contains(input.MIME, "rar") || strings.Contains(input.MIME, "7z") || strings.Contains(input.MIME, "tar") || strings.Contains(input.MIME, "gzip") {
			typeName = "压缩包"
		} else if strings.Contains(input.MIME, "pdf") || strings.Contains(input.MIME, "document") || strings.Contains(input.MIME, "sheet") || strings.Contains(input.MIME, "presentation") {
			typeName = "文档"
		}
	}
	if typeName != "" {
		result = append(result, Candidate{Name: typeName, CategoryID: "file_type", SourceType: models.TagSourceMetadata, SourceKey: "mime", Priority: 75, Weight: 1})
	}
	if input.IsEnc {
		result = append(result, Candidate{Name: "加密", CategoryID: "file_type", SourceType: models.TagSourceMetadata, SourceKey: "encrypted", Priority: 70, Weight: 1})
	}
	if input.Size > 0 {
		sizeName := "小文件"
		if input.Size >= 1024*1024*1024 {
			sizeName = "大文件"
		} else if input.Size >= 100*1024*1024 {
			sizeName = "中型文件"
		}
		result = append(result, Candidate{Name: sizeName, CategoryID: "other", SourceType: models.TagSourceMetadata, SourceKey: "size", Priority: 10, Weight: 0.5})
	}
	return result
}

func metadataCategory(key string) string {
	switch strings.ToLower(key) {
	case "resolution":
		return "resolution"
	case "codec", "video_codec", "audio_codec", "container", "format":
		return "codec"
	case "language":
		return "language"
	case "year":
		return "year"
	default:
		return ""
	}
}

func normalizeKnownTag(value, categoryID string) string {
	normalized := Normalize(value)
	if categoryID == "resolution" {
		switch normalized {
		case "2160p", "4k":
			return "4K"
		case "4320p", "8k":
			return "8K"
		case "1080p":
			return "1080P"
		case "720p":
			return "720P"
		}
	}
	if categoryID == "codec" {
		switch strings.ReplaceAll(normalized, ".", "") {
		case "h265", "x265", "hevc":
			return "H265"
		case "h264", "x264", "avc":
			return "H264"
		case "av1":
			return "AV1"
		}
	}
	return value
}

func Normalize(value string) string {
	value = norm.NFKC.String(value)
	value = strings.TrimSpace(value)
	value = strings.Join(strings.Fields(value), " ")
	return strings.ToLower(value)
}

func displayTagName(value string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(norm.NFKC.String(value)), " "))
}

// DisplayName 对外提供与自动标签一致的 NFKC 和空白归一化，但不应用别名或停用词。
func DisplayName(value string) string {
	return displayTagName(value)
}

func ValidTagName(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) == 0 || len(runes) > MaxTagRunes {
		return false
	}
	for _, char := range runes {
		if unicode.IsControl(char) || char == rune(0xFEFF) {
			return false
		}
	}
	return true
}

// IsPureNumericTagName 判断标签归一化后是否只包含数字。
func IsPureNumericTagName(value string) bool {
	runes := []rune(displayTagName(value))
	if len(runes) == 0 {
		return false
	}
	for _, char := range runes {
		if !unicode.IsDigit(char) {
			return false
		}
	}
	return true
}

func validSegment(value string) bool {
	value = strings.TrimSpace(value)
	if !ValidTagName(value) {
		return false
	}
	runes := []rune(value)
	if len(runes) == 1 && !unicode.IsDigit(runes[0]) {
		return false
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil && len(value) < 4 {
		return false
	}
	for _, char := range runes {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			return true
		}
	}
	return false
}

func preprocessTitle(value string) string {
	value = norm.NFKC.String(value)
	value = acronymBoundaryPattern.ReplaceAllString(value, `${1} ${2}`)
	value = lowerUpperBoundaryPattern.ReplaceAllString(value, `${1} ${2}`)
	value = letterDigitPattern.ReplaceAllString(value, `${1} ${2}`)
	value = digitLetterPattern.ReplaceAllString(value, `${1} ${2}`)
	value = strings.Map(func(char rune) rune {
		if unicode.IsSpace(char) || unicode.IsPunct(char) || unicode.IsSymbol(char) || unicode.IsControl(char) {
			return ' '
		}
		return char
	}, value)
	return Normalize(value)
}

func validFilenameToken(token SegmentToken, tokenizer Tokenizer, stopWords map[string]struct{}) bool {
	word := displayTagName(token.Text)
	normalized := Normalize(word)
	if !ValidTagName(word) || IsPureNumericTagName(word) || utf8.RuneCountInString(word) < 2 {
		return false
	}
	if _, stopped := defaultFilenameStopWords[normalized]; stopped {
		return false
	}
	if _, stopped := stopWords[normalized]; stopped {
		return false
	}
	if tokenizer != nil && (tokenizer.IsStopWord(word) || tokenizer.IsStopWord(normalized)) {
		return false
	}
	hasHan := false
	allLetters := true
	for _, char := range word {
		if unicode.Is(unicode.Han, char) {
			hasHan = true
		}
		if !unicode.IsLetter(char) {
			allLetters = false
		}
	}
	if hasHan {
		_, allowed := allowedFilenamePOS[strings.ToLower(strings.TrimSpace(token.POS))]
		return allowed
	}
	return allLetters
}
