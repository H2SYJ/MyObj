# MyObj TinyGo 插件 SDK

本 SDK 用于在独立 Go 项目中开发 MyObj ABI v2 订阅插件。完整的 manifest 字段、权限、下载条目、宿主 HTTP、文件查询、自定义请求头、目录、缩略图、打包升级和排错说明，请阅读[插件开发手册](../../docs/plugin-development.md)。

## 快速开始

```bash
go mod init example.com/my-plugin
go get github.com/H2SYJ/MyObj/sdk/tinygo@latest
tinygo build -target=wasip1 -opt=z -o plugin.wasm .
myobj-cli plugin pack . example.myobj-plugin
myobj-cli plugin validate example.myobj-plugin
```

插件入口实现 `Handler` 并调用 `Run`：

```go
type Handler interface {
    Healthcheck() error
    ValidateConfig(map[string]interface{}) error
    Fetch(InvocationRequest) ([]DownloadableItem, error)
}

func main() {
    myobjplugin.Run(handler{})
}
```

## SDK 能力

- `HTTPRequest`：经 MyObj 公网 URL 安全策略访问 HTTP/HTTPS 数据源，需要 `network.public_http`。
- `FileGet`：按当前用户的 `uf_id` 查询单个安全文件元数据，需要 `files.read_metadata`。
- `FilesQuery`：按保存目录内的根相对目录、精确或包含名称、MIME、时间等条件分页查询文件元数据，需要 `files.read_metadata`；宿主不会返回订阅保存目录之外的文件。默认预留 2 MiB 响应缓冲区，可通过 `FileQuery.MaxResponseBytes` 调整为 64 KiB 至 2 MiB。
- `DownloadableItem`：返回 HTTP/HLS URL、稳定 ID、保存目录下不带前导 `/` 的 `relative_save_path`、缩略图和离线下载自定义头。

插件不能直接访问宿主数据库、文件系统或原生网络。WASI 不预挂载目录；权限必须先在 `manifest.json` 声明，并在安装和订阅阶段获得授权。

## 关键限制

- 单次 WASM 执行最长 60 秒，内存上限 64 MiB。
- stdout 最多 2 MiB，stderr 最多 256 KiB。
- 宿主 HTTP 调用不限制次数；新 SDK 默认允许 2 MiB 响应，可通过 `HTTPRequestInput.MaxResponseBytes` 调整为 64 KiB 至 4 MiB。宿主为兼容旧插件保留 10 MiB 全局硬上限。
- `FileGet` 和 `FilesQuery` 不限制调用次数，累计最多返回 500 条；频繁的小结果查询应设置尽可能小的 `FileQuery.MaxResponseBytes`，避免耗尽 64 MiB WASM 内存。
- `healthcheck` 和 `validate_config` 在无宿主权限环境运行，不能访问网络或查询文件。
- 非 `wasip1` 原生测试中调用宿主函数会返回 `host_call_requires_wasip1`；应把解析逻辑拆成纯函数测试。

插件包由根目录的 `manifest.json`、`plugin.wasm`、CLI 自动生成的 `checksums.sha256`，以及可选 README 和图标组成。密码或令牌配置字段应标记 `secret: true`，但插件仍不得把秘密写入日志或错误。
