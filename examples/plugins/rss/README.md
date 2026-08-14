# RSS/Atom 示例订阅插件

这是一个可编译的 ABI v2 示例，展示如何校验配置、通过宿主 HTTP 接口读取 RSS/Atom、解析 `enclosure`、返回 HTTP/HLS 条目、相对保存目录、发布时间和公开缩略图。

开始修改前建议先阅读[完整插件开发手册](../../../docs/plugin-development.md)。

## 本地构建

```bash
go test ./...
tinygo build -target=wasip1 -opt=z -o plugin.wasm .
myobj-cli plugin pack . rss.myobj-plugin
myobj-cli plugin validate rss.myobj-plugin
```

示例的 `go.mod` 使用本地 `replace` 引用仓库内 SDK，因此应从当前目录构建。复制到独立仓库时，请删除本地 `replace`，并按开发手册改用 SDK 自身声明的 module 路径。

## 示例行为

- `feed_url` 是数据源，变化会开始新的来源代次。
- `relative_save_path` 是订阅保存目录下不带前导 `/` 的相对目录。
- RSS 读取 `item/enclosure@url`，Atom 读取 `link rel="enclosure"`。
- URL 路径以 `.m3u8` 结尾时返回 `hls`，其他返回 `http`。
- RSS 的缩略图 URL 直接作为公开 `thumbnail_url` 返回，不携带 feed 或下载请求头。
- 插件只声明 `network.public_http`，不查询文件元数据，也不返回离线下载自定义头。

发布时只分发生成的 `.myobj-plugin`。不要单独分发 `plugin.wasm` 让管理员手工拼包，因为安装器还会校验 manifest、checksums、包哈希和权限。
