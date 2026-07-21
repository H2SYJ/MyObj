# MyObj TinyGo 插件 SDK

插件是独立项目，可通过 Go module 引用本目录：

```bash
go get github.com/H2SYJ/MyObj/sdk/tinygo@latest
tinygo build -target=wasip1 -opt=z -o plugin.wasm .
myobj-cli plugin pack . example.myobj-plugin
myobj-cli plugin validate example.myobj-plugin
```

入口调用 `myobjplugin.Run(handler)`。插件只能使用 manifest 已声明且订阅已授权的 `HTTPRequest`、`FileGet` 和 `FilesQuery` 宿主接口；WASI 不预挂载目录，也不提供原生网络。

`manifest.json`、`plugin.wasm` 和自动生成的 `checksums.sha256` 会一起写入安装包。配置中的密码字段应在 manifest 中标记 `secret: true`。
