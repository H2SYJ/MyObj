package tagging

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/go-ego/gse"
)

func TestEmbeddedJiebaBigDictionaryIntegrity(t *testing.T) {
	if !utf8.ValidString(embeddedJiebaBigDictionary) {
		t.Fatal("嵌入的jieba大词典不是有效UTF-8")
	}
	if strings.HasPrefix(embeddedJiebaBigDictionary, string(rune(0xFEFF))) {
		t.Fatal("嵌入的jieba大词典包含UTF-8 BOM")
	}
	sum := sha256.Sum256([]byte(embeddedJiebaBigDictionary))
	if actual := hex.EncodeToString(sum[:]); actual != jiebaBigDictionarySHA256 {
		t.Fatalf("嵌入的jieba大词典哈希错误: %s", actual)
	}
}

func TestPrimaryDictionaryWinsOverBuiltInDuplicates(t *testing.T) {
	segmenter := &gse.Segmenter{SkipLog: true}
	if err := loadBaseTagDictionaries(segmenter, "中国 999999 nz"); err != nil {
		t.Fatal(err)
	}
	for index := range segmenter.Dictionary().Tokens {
		token := &segmenter.Dictionary().Tokens[index]
		if token.Text() == "中国" {
			if token.Freq() != 999999 || token.Pos() != "nz" {
				t.Fatalf("内置词典覆盖了优先词典: freq=%v pos=%s", token.Freq(), token.Pos())
			}
			return
		}
	}
	t.Fatal("优先词典词条未加载")
}

func TestSharedTokenizerInitializesOnceAcrossConcurrentCallers(t *testing.T) {
	const workers = 12
	results := make(chan *GSETokenizer, workers)
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			tokenizer, err := sharedGSETokenizer()
			results <- tokenizer
			errors <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first *GSETokenizer
	for tokenizer := range results {
		if first == nil {
			first = tokenizer
			continue
		}
		if tokenizer != first {
			t.Fatal("并发调用返回了不同的基础分词器实例")
		}
	}
}
