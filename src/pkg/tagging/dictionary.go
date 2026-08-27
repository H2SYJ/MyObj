package tagging

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-ego/gse"
)

const jiebaBigDictionarySHA256 = "b16011275c42955ccd81fc1adecc93a59dbb7926af69d93fc95d4943d40f6aad"

//go:embed data/dict.txt.big
var embeddedJiebaBigDictionary string

var (
	sharedTokenizerOnce sync.Once
	sharedTokenizer     *GSETokenizer
	sharedTokenizerErr  error
)

func sharedGSETokenizer() (*GSETokenizer, error) {
	sharedTokenizerOnce.Do(func() {
		started := time.Now()
		sharedTokenizer, sharedTokenizerErr = buildSharedGSETokenizer()
		if sharedTokenizerErr == nil {
			log.Printf("标签基础词典已加载: tokens=%d duration=%s", sharedTokenizer.segmenter.Dictionary().NumTokens(), time.Since(started))
		}
	})
	return sharedTokenizer, sharedTokenizerErr
}

func buildSharedGSETokenizer() (*GSETokenizer, error) {
	if !utf8.ValidString(embeddedJiebaBigDictionary) {
		return nil, errors.New("jieba大词典不是有效的UTF-8文本")
	}
	if strings.HasPrefix(embeddedJiebaBigDictionary, string(rune(0xFEFF))) {
		return nil, errors.New("jieba大词典不能包含UTF-8 BOM")
	}
	sum := sha256.Sum256([]byte(embeddedJiebaBigDictionary))
	if actual := hex.EncodeToString(sum[:]); actual != jiebaBigDictionarySHA256 {
		return nil, fmt.Errorf("jieba大词典哈希不匹配: %s", actual)
	}
	segmenter := &gse.Segmenter{SkipLog: true}
	if err := loadBaseTagDictionaries(segmenter, embeddedJiebaBigDictionary); err != nil {
		return nil, err
	}
	if err := segmenter.LoadStopEmbed("zh"); err != nil {
		return nil, fmt.Errorf("加载GSE内置停用词失败: %w", err)
	}
	return &GSETokenizer{segmenter: segmenter}, nil
}

func loadBaseTagDictionaries(segmenter *gse.Segmenter, primary string) error {
	if segmenter == nil {
		return errors.New("标签分词器不能为空")
	}
	if err := segmenter.LoadDictStr(primary); err != nil {
		return fmt.Errorf("加载jieba大词典失败: %w", err)
	}
	if err := segmenter.LoadDictEmbed("zh"); err != nil {
		return fmt.Errorf("加载GSE内置补充词典失败: %w", err)
	}
	return nil
}
