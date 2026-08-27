# jieba 大词典

`dict.txt.big` 来自 [fxsjy/jieba](https://github.com/fxsjy/jieba/blob/master/extra_dict/dict.txt.big)，用于补充分词词频与词性。

- 编码：UTF-8（无 BOM）
- SHA-256：`b16011275c42955ccd81fc1adecc93a59dbb7926af69d93fc95d4943d40f6aad`
- 许可证：MIT，详见 `LICENSE.jieba`

程序通过 `go:embed` 将词典编译进二进制，运行时不读取外部词典文件。
