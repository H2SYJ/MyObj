# MyObj 可安装订阅插件开发手册

本文面向插件作者，说明如何在独立项目中开发、测试、打包和发布 MyObj 订阅插件。本文以 ABI v1 和仓库内 TinyGo SDK 的当前实现为准。

插件的职责是发现“可下载的数据”，而不是自己执行离线下载。插件在定时运行时访问数据源、解析条目并返回下载描述；MyObj 负责去重、权限校验、创建离线下载任务、断点续传、保存文件和处理缩略图。

## 1. 先了解运行模型

```mermaid
flowchart LR
    A["每日调度或手动运行"] --> B["启动 WASM 插件"]
    B --> C["stdin 写入 ABI v1 JSON"]
    C --> D["插件通过宿主 API 拉取数据和查询文件元数据"]
    D --> E["stdout 返回 DownloadableItem 列表"]
    E --> F["权限、URL、目录和请求头校验"]
    F --> G["去重并创建 HTTP/HLS 离线任务"]
    G --> H["下载成功后创建虚拟目录并入库"]
    H --> I["异步拉取和替换缩略图"]
```

每个插件运行都是一次新的 WASM 实例。插件不能依赖进程内全局变量在两次运行之间保存状态。持久状态应来自插件配置、远端数据源，或通过文件查询接口观察到的用户文件元数据。

### 1.1 安全边界

插件运行在 `wazero + WASI Preview 1` 沙箱中：

- 不挂载宿主目录，不能直接读取 MyObj 文件系统。
- 不能访问数据库、服务端配置、其他插件或其他订阅。
- 不提供原生套接字网络，只能使用 `HTTPRequest` 宿主接口。
- 文件接口只返回当前订阅所属用户的安全元数据，不返回文件内容和物理存储信息。
- 宿主接口同时受 manifest 声明权限和订阅授权约束。
- 单次执行最长 60 秒，WASM 内存上限为 1024 页，即 64 MiB。
- stdout 最多 2 MiB，stderr 最多 256 KiB。

WASI 仍用于标准输入、标准输出、时间等基础能力，但没有目录预打开，也没有原生网络能力。不要尝试从插件直接调用 `net/http`；TinyGo 的 `net/http` 不会自动转发到 MyObj 宿主网络接口。

### 1.2 两类请求头不要混淆

插件开发中存在两类用途完全不同的请求头：

1. `HTTPRequestInput.Headers`：仅用于本次插件运行期间访问 RSS、API 等数据源。
2. `DownloadableItem.RequestHeaders`：随条目返回，由 MyObj 加密保存，供之后的 HTTP/HLS 离线下载使用。

前者不会自动传给下载器，后者也不会用于插件自己的数据源请求或缩略图请求。需要两边都认证时，插件必须分别设置。

## 2. 开发环境

建议使用以下环境：

- Go 1.23 或更高版本，用于模块管理和本地单元测试。
- TinyGo 0.39 或与项目验证版本兼容的更新版本。
- MyObj CLI，用于生成和校验 `.myobj-plugin`。

TinyGo 请从[官方安装说明](https://tinygo.org/getting-started/install/)或对应版本的 release 安装。Windows、Linux 和 macOS 都需要把 `tinygo` 可执行文件加入 `PATH`。不要使用普通 Go 的 `GOOS=js GOARCH=wasm` 代替 TinyGo `wasip1` 目标，两者导入和启动约定不同。

确认工具可用：

```bash
go version
tinygo version
myobj-cli --help
```

还可以确认目标是否存在：

```bash
tinygo build -target=wasip1 -o healthcheck.wasm ./path/to/a/minimal/plugin
```

如果团队不希望在宿主机安装 TinyGo，可以固定官方容器版本。Linux/macOS shell：

```bash
docker run --rm \
  -e GOFLAGS=-buildvcs=false \
  -v "$PWD:/src" \
  -w /src \
  tinygo/tinygo:0.39.0 \
  tinygo build -target=wasip1 -opt=z -o plugin.wasm .
```

Windows PowerShell：

```powershell
docker run --rm `
  -e GOFLAGS=-buildvcs=false `
  -v "${PWD}:/src" `
  -w /src `
  tinygo/tinygo:0.39.0 `
  tinygo build -target=wasip1 -opt=z -o plugin.wasm .
```

固定容器标签可以让 CI 和本地使用同一编译器。挂载 Git 工作区时设置 `GOFLAGS=-buildvcs=false`，可避免容器内 Git 所有权或 VCS 元数据检测影响编译。

从 MyObj 源码构建 CLI：

```bash
go build -o myobj-cli ./src/cmd/cli
```

Windows 下生成的文件通常是 `myobj-cli.exe`。后续示例统一写作 `myobj-cli`，请按实际路径调用。

## 3. 创建独立插件项目

插件可以放在完全独立的 Git 仓库中。一个推荐结构如下：

```text
my-download-plugin/
├── go.mod
├── go.sum
├── main.go
├── manifest.json
├── README.md
├── icon.png
├── internal/
│   ├── client/
│   └── parser/
└── plugin.wasm          # 编译产物，不一定提交到 Git
```

初始化模块：

```bash
mkdir my-download-plugin
cd my-download-plugin
go mod init example.com/my-download-plugin
go get github.com/H2SYJ/MyObj/sdk/tinygo@latest
```

当前 SDK 自身声明的 Go module 路径是 `github.com/H2SYJ/MyObj/sdk/tinygo`，独立项目的 import 必须与该路径一致。正式发布插件时，建议将 `latest` 换成与目标 MyObj 版本对应的标签或提交，避免 SDK 自动升级导致行为变化。

如果正在 MyObj 源码树旁开发，可以临时引用本地 SDK：

```go
module example.com/my-download-plugin

go 1.23

require github.com/H2SYJ/MyObj/sdk/tinygo v0.0.0

replace github.com/H2SYJ/MyObj/sdk/tinygo => ../MyObj/sdk/tinygo
```

`replace` 只应用于本地构建，不会被打进 WASM。发布的是编译后的 WASM 和插件包，目标 MyObj 服务端不需要联网下载 Go module。

## 4. 最小可运行插件

`main.go`：

```go
package main

import (
    "fmt"
    "strings"

    myobjplugin "github.com/H2SYJ/MyObj/sdk/tinygo"
)

type handler struct{}

func (handler) Healthcheck() error {
    return nil
}

func (handler) ValidateConfig(config map[string]interface{}) error {
    sourceURL, _ := config["source_url"].(string)
    if strings.TrimSpace(sourceURL) == "" {
        return fmt.Errorf("数据源地址不能为空")
    }
    return nil
}

func (h handler) Fetch(request myobjplugin.InvocationRequest) ([]myobjplugin.DownloadableItem, error) {
    if err := h.ValidateConfig(request.Config); err != nil {
        return nil, err
    }

    // 最小示例直接返回一个条目。实际插件通常先调用 HTTPRequest 拉取数据源。
    return []myobjplugin.DownloadableItem{
        {
            ID:           "example-item-001",
            Title:        "示例文件",
            URL:          "https://downloads.example.com/files/example.zip",
            DownloadType: "http",
            SavePath:     "/离线下载/示例插件",
        },
    }, nil
}

func main() {
    myobjplugin.Run(handler{})
}
```

`manifest.json`：

```json
{
  "id": "com.example.downloads",
  "name": "示例下载订阅",
  "version": "1.0.0",
  "api_version": "1",
  "author": "Example Team",
  "description": "返回示例 HTTP 下载条目",
  "config_fields": [
    {
      "key": "source_url",
      "label": "数据源地址",
      "description": "插件用于发现条目的 HTTP/HTTPS API",
      "type": "text",
      "required": true,
      "affects_source": true
    }
  ]
}
```

编译、打包、校验：

```bash
tinygo build -target=wasip1 -opt=z -o plugin.wasm .
myobj-cli plugin pack . example.myobj-plugin
myobj-cli plugin validate example.myobj-plugin
```

`plugin validate` 会校验 ZIP、manifest、校验和、WASM 格式和 `healthcheck`。它不会执行需要真实订阅配置和授权的 `fetch`，因此通过校验不代表数据源解析一定正确。

## 5. manifest.json 完整说明

manifest 必须是 UTF-8 无 BOM 的单个 JSON 对象。未知字段、尾随第二个 JSON 值、错误类型和不支持的权限都会导致安装失败。

### 5.1 顶层字段

| 字段 | 必填 | 规则与用途 |
| --- | --- | --- |
| `id` | 是 | 稳定插件 ID，3–128 个字符；首字符为小写字母，其余只能是小写字母、数字、`.`、`_`、`-`。安装升级时不可改变。 |
| `name` | 是 | 展示名称，不能为空。 |
| `version` | 是 | 版本格式为 `主版本.次版本.修订版本`，可带 `-预发布标识`，例如 `1.2.0-beta.1`；当前不接受 `+build` 元数据。升级版本必须高于已安装版本。 |
| `api_version` | 是 | 当前固定为字符串 `"1"`。 |
| `author` | 否 | 作者或组织名称。 |
| `description` | 否 | 管理员和用户看到的功能说明。 |
| `min_myobj_version` | 否 | 声明建议的最低 MyObj 版本。当前安装器保留此信息，但不会替插件完成全部兼容性判断，仍应在目标版本上测试。 |
| `permissions` | 否 | 权限字符串数组，不得重复，只能使用本文列出的权限。 |
| `config_fields` | 否 | 订阅配置表单定义。每条订阅保存一份独立配置。 |

### 5.2 权限字段

| 权限 | 能力 | 何时需要 |
| --- | --- | --- |
| `network.public_http` | 调用 `HTTPRequest` 访问公网 HTTP/HTTPS | RSS、REST API、GraphQL 或页面抓取 |
| `files.read_metadata` | 调用 `FileGet`、`FilesQuery` 查询当前用户文件元数据 | 根据已存在文件决定是否返回条目 |
| `downloads.custom_headers` | 在 `DownloadableItem` 中返回离线下载请求头 | 下载地址需要 Cookie、Authorization、Referer 等 |

权限需要管理员在安装时审核。创建订阅时，用户还需确认文件元数据查询和自定义下载头权限。`network.public_http` 在插件声明后会随订阅自动纳入授权，因为插件无法通过其他方式访问数据源。

不要声明没有使用的权限。插件升级新增权限后，已有订阅会进入 `needs_permission` 并暂停，直到用户重新确认。

### 5.3 config_fields 字段

每个配置字段支持：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `key` | 是 | 1–64 个字符；以 ASCII 字母开头，后续可使用字母、数字、`_`、`.`、`-`；同一 manifest 内不能重复。 |
| `label` | 建议 | 用户界面标签。虽然解析器目前不强制非空，但发布插件不应省略。 |
| `description` | 否 | 输入提示和用途说明。 |
| `type` | 是 | `text`、`password`、`number`、`boolean`、`select` 之一。 |
| `required` | 否 | 告知界面该字段必填。业务上的必填仍必须由 `ValidateConfig` 检查。 |
| `secret` | 否 | 标记敏感字段。界面只显示“已配置”，更新时空值会保留原值，不回显明文。通常与 `password` 一起使用。 |
| `affects_source` | 否 | 值变化时开始新的来源代次，旧条目的去重记录不阻止新来源首次抓取。源地址、频道 ID、账号等应设为 `true`。 |
| `default` | 否 | 表单默认值，类型应与 `type` 一致。插件仍应为缺失值提供合理兜底，以兼容 API 客户端。 |
| `options` | `select` 建议 | 选项数组，每项包含 `label` 和 `value`。`value` 可以是 JSON 字符串、数字或布尔值。 |

完整示例：

```json
{
  "id": "com.example.media",
  "name": "媒体订阅",
  "version": "1.3.0",
  "api_version": "1",
  "author": "Example Team",
  "description": "从远端 API 发现媒体文件",
  "min_myobj_version": "1.1.0",
  "permissions": [
    "network.public_http",
    "files.read_metadata",
    "downloads.custom_headers"
  ],
  "config_fields": [
    {
      "key": "endpoint",
      "label": "API 地址",
      "description": "必须是公网 HTTP/HTTPS 地址",
      "type": "text",
      "required": true,
      "affects_source": true
    },
    {
      "key": "token",
      "label": "访问令牌",
      "type": "password",
      "required": true,
      "secret": true
    },
    {
      "key": "page_size",
      "label": "每页条数",
      "type": "number",
      "default": 50
    },
    {
      "key": "include_existing",
      "label": "包含已存在文件",
      "type": "boolean",
      "default": false
    },
    {
      "key": "quality",
      "label": "画质",
      "type": "select",
      "default": "1080p",
      "options": [
        { "label": "720p", "value": "720p" },
        { "label": "1080p", "value": "1080p" },
        { "label": "4K", "value": "2160p" }
      ]
    }
  ]
}
```

整个订阅配置都会由服务端加密保存；`secret` 主要控制 API 和界面的回显及更新行为。插件错误、stdout、stderr 和远端请求中仍可能泄漏插件主动打印的秘密，因此不要记录配置值。

## 6. ABI v1 与 Handler

SDK 的 `Run` 从 stdin 读取一个 UTF-8 JSON 请求，调用 `Handler`，再向 stdout 写入一个 JSON 响应。插件应只向 stdout 输出 ABI 响应；调试信息写 stderr。

### 6.1 输入结构

```json
{
  "action": "fetch",
  "config": {
    "endpoint": "https://api.example.com/items",
    "token": "secret"
  },
  "now": "2026-07-22T08:00:00+08:00"
}
```

| 字段 | 说明 |
| --- | --- |
| `action` | `healthcheck`、`validate_config` 或 `fetch`。 |
| `config` | 当前订阅的配置对象；`healthcheck` 时通常省略。 |
| `now` | 本次调用的宿主时间，JSON 时间格式为 RFC 3339。需要“当前时间”时优先使用它，使测试行为更可控。 |

SDK 单次最多读取 2 MiB stdin。正常订阅配置应远小于此限制。

### 6.2 输出结构

成功：

```json
{
  "ok": true,
  "items": [
    {
      "id": "episode-100",
      "title": "第 100 期",
      "url": "https://cdn.example.com/episode-100.mp4",
      "download_type": "http"
    }
  ],
  "message": "发现 1 个条目"
}
```

失败：

```json
{
  "ok": false,
  "error": "配置中的频道不存在"
}
```

使用 SDK 时不要自行编码顶层响应，返回 Go error 即可。SDK 会将 `err.Error()` 放进 `error`。错误消息应便于用户理解，但不能包含令牌、Cookie、完整带查询参数 URL 或文件列表。

### 6.3 三个 Handler 方法

#### Healthcheck

```go
Healthcheck() error
```

安装、CLI 校验时调用。它必须快速完成，只检查插件自身能否启动、静态资源是否完整、内部初始化是否正常。此动作没有任何宿主权限，不能调用 `HTTPRequest`、`FileGet` 或 `FilesQuery`。

#### ValidateConfig

```go
ValidateConfig(map[string]interface{}) error
```

创建订阅、更新配置以及插件升级兼容性检查时调用。它应检查：

- 必填值是否存在。
- JSON 类型是否符合预期。
- URL、枚举、数字范围和字段组合是否合法。
- 新插件版本是否仍接受旧订阅配置。

该动作同样没有宿主权限。不要通过远端请求验证令牌；远端可用性检查应放在 `Fetch`。升级时只要任一已有订阅无法通过新版本的 `ValidateConfig`，整个插件升级就会被拒绝。

从 `map[string]interface{}` 读取 JSON 数字时，默认动态类型通常是 `float64`：

```go
pageSize, ok := config["page_size"].(float64)
if !ok || pageSize < 1 || pageSize > 100 {
    return fmt.Errorf("每页条数必须在 1 到 100 之间")
}
```

#### Fetch

```go
Fetch(InvocationRequest) ([]DownloadableItem, error)
```

定时或手动运行订阅时调用。典型流程是：

1. 再次调用 `ValidateConfig` 做纯本地校验。
2. 用 `HTTPRequest` 获取数据源。
3. 必要时用 `FileGet` 或 `FilesQuery` 检查已有文件。
4. 解析并返回条目，稳定排序并尽量给出 `published_at`。

MyObj 会按 `published_at` 从新到旧稳定排序，无时间的条目排在有时间条目之后；随后最多处理 500 条。首次运行只提交订阅配置的“首次下载数量”，以后每次受“单次任务上限”限制。插件最好在源端分页时就控制结果规模，不要依赖 2 MiB stdout 上限裁剪。

## 7. DownloadableItem 字段

| 字段 | 必填 | 规则与行为 |
| --- | --- | --- |
| `id` | 强烈建议 | 数据源内稳定、不可复用的条目 ID。相同来源代次内用于去重。缺失时宿主根据规范化 URL 生成键。 |
| `title` | 否 | 展示标题，不参与去重。 |
| `url` | 是 | 公网 HTTP/HTTPS 下载地址，私网、localhost、链路本地和保留地址会被拒绝；重定向同样校验。 |
| `published_at` | 否 | RFC 3339 时间。用于首次抓取时优先选择最新条目。 |
| `download_type` | 是 | 只能是 `http` 或 `hls`，使用小写。 |
| `file_name` | 否 | 输出文件名，不得含路径分隔符或 NUL，UTF-8 字节数不超过 255。HTTP 留空时由响应或 URL 推断；HLS 最终总是规范化为 `.mp4`。 |
| `save_path` | 否 | 用户虚拟空间绝对目录。留空时使用订阅默认目录。 |
| `thumbnail_url` | 否 | 公网 HTTP/HTTPS 图片地址；不共享主文件请求头。 |
| `request_headers` | 否 | 当前下载条目的自定义 HTTP 请求头字符串对象，需要 `downloads.custom_headers`。 |
| `header_hosts` | 否 | 可注入上述请求头的额外精确主机名数组；下载 URL 自身主机自动加入。 |

完整条目示例：

```go
published := time.Date(2026, 7, 22, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))

item := myobjplugin.DownloadableItem{
    ID:           "video:av123:1080p",
    Title:        "示例视频",
    URL:          "https://media.example.com/master.m3u8",
    PublishedAt:  &published,
    DownloadType: "hls",
    FileName:     "示例视频.mp4",
    SavePath:     "/订阅/示例频道/2026",
    ThumbnailURL: "https://images.example.com/av123.webp",
    RequestHeaders: map[string]string{
        "Authorization": "Bearer " + token,
        "Referer":       "https://www.example.com/",
    },
    HeaderHosts: []string{
        "segments.examplecdn.com",
        "keys.examplecdn.com",
    },
}
```

### 7.1 稳定 ID 和去重

如果 `id` 非空，宿主使用 `SHA-256("id:" + strings.TrimSpace(id))` 生成条目键。插件升级后应继续返回相同 ID，否则系统会认为它是新条目并可能重复下载。

如果 `id` 为空，宿主会：

- 去掉 URL fragment。
- 将 scheme 和整个 host 转为小写。
- 空路径规范为 `/`。
- 保留查询参数。
- 对 `"url:" + 规范化URL` 计算 SHA-256。

带时效签名的 URL 经常改变查询参数，因此不能依赖 URL 去重。此类数据源必须返回稳定 `id`，并在每次 `Fetch` 中为同一 ID 返回最新 URL 和请求头。

`affects_source` 配置变化会增加来源代次。新代次拥有独立去重空间，适用于“换了账号、频道或源地址后需要把新来源视为首次抓取”的场景。

## 8. 数据源 HTTPRequest 宿主接口

manifest 必须声明 `network.public_http`。

```go
response, err := myobjplugin.HTTPRequest(myobjplugin.HTTPRequestInput{
    Method: "POST",
    URL:    "https://api.example.com/graphql",
    Headers: map[string]string{
        "Authorization": "Bearer " + token,
        "Content-Type":  "application/json",
    },
    Body: []byte(`{"query":"{ items { id url } }"}`),
})
if err != nil {
    return nil, fmt.Errorf("请求数据源失败: %w", err)
}
if response.StatusCode < 200 || response.StatusCode >= 300 {
    return nil, fmt.Errorf("数据源返回 %d", response.StatusCode)
}
body, err := response.Body()
if err != nil {
    return nil, fmt.Errorf("解码数据源响应失败: %w", err)
}
```

宿主不限制单次插件运行的 HTTP 调用次数，但插件仍受 60 秒执行超时约束。

其他限制：

- 仅支持 `GET`、`HEAD`、`POST`。
- 请求 body 最多 1 MiB。
- 单个响应 body 最多 10 MiB。
- 最多 32 个请求头，请求头名称和值合计不超过 32 KiB。
- 请求头值不能含 CR/LF。
- 目标和每次重定向必须通过公网 URL 安全检查。
- 请求使用 MyObj 的安全 HTTP 客户端、HTTP 代理和下载限速配置。

该接口禁止插件控制传输层或代理头，包括 `Host`、`Connection`、`Content-Length`、`Transfer-Encoding`、`Keep-Alive`、`TE`、`Trailer`、`Upgrade`、`Forwarded`、`Proxy-*` 和 `X-Forwarded-*`。业务认证头如 `Authorization`、`Cookie`、`Referer`、`Origin`、`User-Agent` 可以使用。

`HTTPResponse.Headers` 是 `map[string][]string`，`BodyBase64` 由 SDK 的 `Body()` 解码。不要直接假定非 2xx 会变成 Go error；HTTP 状态码需要插件自己判断。

## 9. 离线下载自定义请求头

manifest 必须声明 `downloads.custom_headers`，用户还必须为订阅授权。插件未声明或订阅未授权时，只要条目返回非空 `request_headers`，该条目就会被拒绝。

### 9.1 校验规则

- 最多 32 个请求头和 32 个额外主机。
- 请求头名称和值合计最多 32 KiB。
- 名称按大小写不敏感判重，必须是合法 ASCII HTTP token。
- 值不能包含 `\r` 或 `\n`。
- 禁止 `Host`、`Range`、`If-Range`、`Content-Length`、`Transfer-Encoding`、`Accept-Encoding`。
- 禁止 `Connection`、`Keep-Alive`、`TE`、`Trailer`、`Upgrade`、`Forwarded`、`Proxy-*`、`X-Forwarded-*` 等逐跳或代理头。
- 允许 `Authorization`、`Cookie`、`Referer`、`Origin`、`User-Agent` 等业务头。
- `request_headers` 为空时不能单独返回 `header_hosts`。

JSON 对象中以下写法也会被拒绝，因为名称大小写不敏感重复：

```json
{
  "request_headers": {
    "Authorization": "Bearer first",
    "authorization": "Bearer second"
  }
}
```

### 9.2 header_hosts 精确白名单

下载 URL 自身的 hostname 会自动进入白名单。`header_hosts` 只需要列出 HLS 播放列表引用的密钥、初始化分片、媒体分片或 HTTP 重定向可能使用的其他域名。

每一项必须是精确 ASCII 域名或 punycode，例如：

```json
[
  "cdn.example.com",
  "keys.xn--fsqu00a.example"
]
```

不允许：

- `*.example.com` 或 `.example.com`。
- `example.com:8443`。
- `https://example.com/path`。
- `user@example.com`。
- IPv4、IPv6 或 CIDR 网段。

请求头只在当前请求目标的 hostname 精确匹配白名单时注入。跳转到未列出的公网主机可以继续下载，但不会携带任何插件头。不存在“允许所有子域名”或“自动信任所有重定向”的选项。

### 9.3 HTTP 与 HLS 的覆盖范围

同一规则用于：

- HTTP 的 HEAD、GET、Range 分片、恢复和重试。
- HLS 主播放列表、子播放列表、密钥、初始化分片和媒体分片。

如果 HLS 使用多个 CDN 域名，必须逐个精确列入 `header_hosts`。开发测试不能只验证主 `.m3u8` 能打开，还要验证子播放列表、密钥和分片。

### 9.4 凭据刷新

对于会过期的 Cookie、签名和令牌，插件应在每次 `Fetch` 返回同一稳定 `id` 的最新请求头。MyObj 使用服务端 HMAC 摘要判断凭据是否变化，明文请求头不会通过普通 API 回显。

新凭据会按任务状态更新：

- deferred 或尚未提交的条目保存新凭据，之后创建任务时使用。
- 排队任务更新任务密文和主机白名单。
- 因 401/403 或凭据解密失败暂停的任务更新后自动恢复。
- 正在下载的任务会撤销旧执行器并重新排队，保留可复用的 HTTP Range 或 HLS 进度。
- 已完成或已取消任务不会因为凭据变化重新下载。

下载过程中任一需要认证的 HTTP/HLS 请求返回 401/403 时，任务会进入凭据等待状态，而不是进行普通网络重试。下次插件刷新凭据后可以恢复。

## 10. 保存目录

`save_path` 是用户虚拟空间的目录，不是服务端物理路径。

规则：

- 必须以 `/` 开头；`/` 表示用户根目录。
- 允许中文。
- 不允许相对路径、`//` 开头、盘符、UNC、URI、反斜杠、`.`、`..` 和控制字符。
- 最多 20 层。
- 每段最多 100 个 Unicode 字符。
- 总长最多 1000 个 Unicode 字符。
- 连续或结尾的 `/` 会被规范化，例如 `/订阅//视频/` 变为 `/订阅/视频`。

示例：

```text
/                         有效，用户根目录
/订阅/电影/2026           有效
/订阅//电影/              有效，规范化为 /订阅/电影
订阅/电影                 无效，不是绝对路径
C:\Downloads              无效，物理路径和反斜杠
/订阅/../私密              无效，包含 ..
```

插件不需要提前创建目录。MyObj 只在主文件下载成功并准备入库时逐级、并发幂等地创建目录；失败下载不会留下空目录。条目未提供 `save_path` 时使用订阅默认目录。已完成条目后来改变目录不会移动现有文件。

## 11. 缩略图

`thumbnail_url` 只接受公网 HTTP/HTTPS URL。缩略图请求不会携带 `request_headers`，因此首版不支持必须登录才能访问的缩略图；插件应返回可匿名访问的图片 URL。

处理发生在主文件下载成功之后：

- 输入最多 5 MiB。
- 支持 JPEG、PNG、WebP、GIF；动画图片取解码器提供的第一帧。
- 最大 4000 万解码像素。
- 转为白色背景的 RGB JPEG。
- 最长边不超过 1000 像素。
- 输出压缩到 1 MiB 以内。
- 插件缩略图覆盖系统自动生成的缩略图。

网络异常、408、429 和 5xx 分别等待 1、5、30 分钟重试三次；格式、权限、体积和像素限制错误立即失败。缩略图失败不影响主下载成功，用户可手动重试。

同一稳定条目返回新的 `thumbnail_url` 时，MyObj 只重新处理缩略图，不重新下载已完成文件。不要把认证令牌放在缩略图 URL 查询参数中；URL 会被保存，而且生命周期可能长于令牌。

## 12. 查询当前用户文件元数据

manifest 必须声明 `files.read_metadata`，并由用户为订阅授权。

插件只能看到订阅所属用户、未删除且不在回收站中的文件。无法查询其他用户、公开广场或管理员全局文件。`uf_id` 是用户级文件 ID，不是底层 `file_info.id`。

### 12.1 FileGet

```go
file, err := myobjplugin.FileGet("用户文件UFID")
if err != nil {
    if err.Error() == "not_found" {
        // 不存在和越权使用同一结果，不能据此探测其他用户数据。
    }
    return nil, err
}
```

不存在、已删除或不属于当前用户都返回 `not_found`。

### 12.2 FilesQuery

```go
encrypted := false
hasThumbnail := true

result, err := myobjplugin.FilesQuery(myobjplugin.FileQuery{
    Path:         "/订阅/视频",
    Recursive:    true,
    NameContains: "第100期",
    MIMEPrefix:   "video/",
    IsEncrypted:  &encrypted,
    HasThumbnail: &hasThumbnail,
    Limit:        100,
})
if err != nil {
    return nil, err
}

for _, file := range result.Files {
    // 根据安全元数据决定是否返回下载条目。
    _ = file.UFID
}

if result.NextCursor != "" {
    next, err := myobjplugin.FilesQuery(myobjplugin.FileQuery{
        Path:      "/订阅/视频",
        Recursive: true,
        Cursor:    result.NextCursor,
        Limit:     100,
    })
    _ = next
    _ = err
}
```

支持的过滤条件：

| Go 字段 / JSON 字段 | 说明 |
| --- | --- |
| `Path` / `path` | 用户虚拟绝对目录；目录不存在时结果为空。 |
| `Recursive` / `recursive` | `false` 只查当前目录，`true` 包含后代目录。仅在设置 `path` 时有意义。 |
| `NameContains` / `name_contains` | 文件名包含匹配。具体大小写行为受数据库排序规则影响，不应依赖跨数据库一致的大小写折叠。 |
| `MIMEPrefix` / `mime_prefix` | MIME 前缀，例如 `image/`、`video/`。 |
| `IsEncrypted` / `is_encrypted` | 指针布尔值；`nil` 表示不筛选。 |
| `IsPublic` / `is_public` | 指针布尔值；`nil` 表示不筛选。 |
| `HasThumbnail` / `has_thumbnail` | 指针布尔值；`nil` 表示不筛选。 |
| `CreatedAfter` / `created_after` | 创建时间大于等于该 RFC 3339 时间。 |
| `CreatedBefore` / `created_before` | 创建时间小于等于该时间。 |
| `UpdatedAfter` / `updated_after` | 底层文件信息更新时间大于等于该时间。 |
| `UpdatedBefore` / `updated_before` | 底层文件信息更新时间小于等于该时间。 |
| `Cursor` / `cursor` | 上一页返回的不透明 cursor。不要解析、修改或跨用户保存复用。 |
| `Limit` / `limit` | 1–100；缺失或越界时按 100。 |

固定按 `created_at DESC, uf_id ASC` 排序。翻页时必须保持所有过滤条件不变，只替换 `cursor`，否则可能产生遗漏或重复。

### 12.3 返回字段

| 字段 | 说明 |
| --- | --- |
| `uf_id` | 当前用户范围内的文件 ID。 |
| `file_name` | 文件名。 |
| `virtual_path` | 文件所在的用户虚拟目录绝对路径。 |
| `file_size` | 字节数。 |
| `mime_type` | MIME 类型。 |
| `created_at` | 用户文件记录创建时间。 |
| `updated_at` | 文件信息更新时间。 |
| `is_encrypted` | 是否加密存储。 |
| `is_public` | 是否公开。 |
| `has_thumbnail` | 是否已有缩略图。 |

不会返回物理路径、随机存储名、完整哈希、加密哈希、分片签名、文件内容、缩略图内容、密码、空间或账号信息。

### 12.4 调用限额与审计

`FileGet` 和 `FilesQuery` 合计每次插件运行最多调用 10 次，累计最多返回 500 条记录。单页最多 100 条。超限分别返回 `file_query_limit` 或 `file_result_limit`。

宿主会审计插件、版本、订阅、用户、查询摘要、返回数量、耗时和状态，但不记录完整文件列表。插件也不应把文件列表写入 stderr。

## 13. 调度和条目提交行为

调度由订阅配置控制，插件本身不需要实现 cron：

- 每条订阅使用服务器时区下的 `HH:mm`，默认时区为 `Asia/Shanghai`。
- 调度器约每 30 秒检查一次。
- 新建订阅默认立即运行，也可以由调用方关闭立即运行。
- 最多并发运行两个插件，同一订阅不会重叠执行。
- 服务重启后会恢复排队或中断的运行。
- 首次下载数量为 1–100，默认 10。
- 后续单次任务上限为 1–500，默认 100。

每次执行都会重新检查用户状态、`file:offLine` 权限、插件状态和订阅授权。返回条目不等于任务一定创建成功；空间、URL、安全策略和用户权限仍可能拒绝任务。

插件应返回“当前可下载条目的一个有限快照”，而不是维护自己的定时循环。不要在 `Fetch` 中等待下一个发布时间。

## 14. 打包格式与 CLI

`.myobj-plugin` 是 ZIP 文件，根目录包含：

```text
manifest.json              必需
plugin.wasm                必需
checksums.sha256           必需，由 CLI 自动生成
README.md                  可选
icon.png                   可选，也支持 jpg、jpeg、svg、webp
```

CLI `plugin pack` 只收集上述固定名称的根目录文件。插件运行时不能读取包内 README、图标或任意附加数据；运行所需静态表格应编译进 WASM。

```bash
myobj-cli plugin pack <source-dir> [output.myobj-plugin]
```

省略输出路径时，会在源目录内生成与目录同名的 `.myobj-plugin`。CLI 会覆盖同名旧输出，生成全部文件的 SHA-256，并立即按安装规则重新读取校验。

```bash
myobj-cli plugin validate <package.myobj-plugin>
```

包限制：

- ZIP 最大 20 MiB。
- 解压后总大小最大 50 MiB。
- `manifest.json` 最大 1 MiB。
- `plugin.wasm` 最大 40 MiB。
- 拒绝绝对路径、目录穿越、反斜杠路径、符号链接和重复文件。
- `manifest.json` 必须 UTF-8 无 BOM。
- `checksums.sha256` 必须覆盖包内除自身外的每个文件，且不能引用不存在或重复的条目。

包目前不要求数字签名。管理员安装时会看到包 SHA-256、WASM SHA-256、声明权限和“未签名、管理员信任安装”警告，并必须明确确认全部权限及信任安装。

## 15. 安装、升级和兼容性

### 15.1 安装

管理员在插件中心上传 `.myobj-plugin`。安装过程会：

1. 校验 ZIP、manifest 和 checksums。
2. 编译检查 WASM。
3. 在无权限环境运行 `healthcheck`。
4. 要求管理员逐项确认 manifest 权限和未签名信任。
5. 保存包和 WASM，再复核二者 SHA-256。

安装成功后，用户才能基于插件创建各自独立配置的订阅。

### 15.2 升级

升级包必须保持相同 `id`，并提供严格更高的 `version`。升级前宿主会用新 WASM 对所有已有订阅运行 `validate_config`；任一失败都会阻止升级。

兼容升级建议：

- 新配置字段提供默认值，并允许旧配置缺失该字段。
- 字段重命名时至少保留一个迁移版本，同时接受旧 key。
- 不要改变已有 `id` 的业务含义。
- 不要改变稳定条目 ID 的生成规则。
- 新增权限前在发行说明中解释原因；已有订阅不会自动获得新权限。
- 如果权限能力是可选的，未授权时优雅降级，而不是让全部抓取失败。

升级新增权限会把已有订阅设为 `needs_permission` 并停用。用户确认后才能恢复。管理员停用插件时订阅状态为 `plugin_unavailable`；插件重新启用后，非 `needs_permission` 订阅恢复可用。

## 16. 测试策略

### 16.1 纯 Go 单元测试

把解析、字段映射、ID 生成等逻辑拆到不依赖宿主接口的函数中：

```go
func parseItems(body []byte) ([]myobjplugin.DownloadableItem, error) {
    // 可在普通 go test 中测试。
}
```

然后执行：

```bash
go test ./...
```

非 `wasip1` 构建中直接调用 `HTTPRequest`、`FileGet`、`FilesQuery` 会返回 `host_call_requires_wasip1`。建议让 Handler 只负责薄编排，把 HTTP 响应解析和业务判断做成可注入、可测试的函数。

至少测试：

- 空配置、错误类型、边界数字和未知枚举。
- RSS/API 空响应、坏 JSON/XML、重复项和缺字段。
- 稳定 ID 在 URL 凭据变化后不变。
- HTTP/HLS 类型映射和文件名。
- 多域 HLS 的 `header_hosts`。
- 文件查询结果为空、分页和达到限额时的降级。

### 16.2 WASM 和安装包测试

```bash
tinygo build -target=wasip1 -opt=z -o plugin.wasm .
myobj-cli plugin pack . test.myobj-plugin
myobj-cli plugin validate test.myobj-plugin
```

最后在测试 MyObj 实例中安装，创建覆盖每组权限的订阅，执行一次手动运行并检查：

- 运行记录中的发现、创建、跳过和错误数量。
- HTTP/HLS 任务是否收到正确的请求头且跨域跳转会剥离。
- 401/403 后刷新同一条目凭据是否恢复。
- 文件是否进入预期目录。
- 主文件成功后缩略图是否异步完成。

在 Docker 挂载 Git 仓库进行 TinyGo 编译时，如果遇到 VCS 所有权检测错误，可为构建容器设置：

```bash
GOFLAGS=-buildvcs=false tinygo build -target=wasip1 -opt=z -o plugin.wasm .
```

## 17. 错误处理和常见错误码

插件可以返回自己的中文错误消息。宿主接口会返回较稳定的机器错误字符串：

| 错误 | 含义 | 建议处理 |
| --- | --- | --- |
| `permission_denied` | manifest 未声明、订阅未授权或运行中权限被撤销 | 停止当前能力；可选能力应降级，必需能力返回清晰错误 |
| `response_too_large` | HTTP 响应超过 10 MiB | 使用源端分页或请求更小字段集 |
| `method_not_allowed` | 使用了 GET/HEAD/POST 以外方法 | 调整数据源协议或通过 POST 表达操作 |
| `invalid_url` | URL 无效或未通过公网安全策略 | 检查 scheme、DNS 和重定向目标 |
| `invalid_header` | 数据源请求头名称、值、数量或大小不合法 | 移除逐跳头并检查 CR/LF |
| `invalid_body` | body 不是有效编码或超过 1 MiB | 缩小请求体 |
| `not_found` | 文件不存在、已删除或不属于当前用户 | 按不存在处理，不要继续探测 |
| `invalid_cursor` | cursor 损坏、被修改或不属于当前用户 | 丢弃 cursor，从第一页重新查询 |
| `file_query_limit` | `FileGet` 与 `FilesQuery` 合计超过 10 次 | 用分页和本地集合减少调用 |
| `file_result_limit` | 累计返回超过 500 条 | 收紧过滤条件和页数 |
| `host_call_failed` | 宿主调用写入缓冲区失败或低层 ABI 异常 | 减少返回数据；若持续出现，记录不含秘密的上下文并报告兼容性问题 |
| `host_call_requires_wasip1` | 在普通 Go 原生构建中调用宿主函数 | 使用纯逻辑单测，实际调用编译为 `wasip1` |

不要根据错误字符串无限重试。一次 `Fetch` 最长 60 秒，插件内部长重试只会占满执行时间。对于远端 429/5xx，通常返回简洁错误，让下一次定时或手动运行重试。

### 17.1 常见故障

#### `plugin validate` 通过，但创建订阅失败

CLI 只运行 `healthcheck`。检查 `ValidateConfig` 是否错误假设配置类型，尤其是把 JSON number 断言为 `int`。

#### TinyGo 编译提示 wasm import 不能作为函数值

不要包装、赋值或传递 SDK 的底层 `//go:wasmimport` 函数。正常插件只调用公开的 `HTTPRequest`、`FileGet`、`FilesQuery`，不要复制 `host_wasip1.go` 的内部实现。

#### 数据源请求返回 `permission_denied`

确认 manifest 声明了 `network.public_http`，重新打包并由管理员升级安装。`healthcheck` 和 `validate_config` 永远没有网络权限，把远端请求移到 `Fetch`。

#### HLS 主列表成功但分片 401/403

检查实际子播放列表、密钥和分片 hostname，把每个需要认证头的域名精确加入 `header_hosts`。不要填写 URL、端口或通配符。

#### 每次运行都重复创建任务

给条目返回稳定 `id`。不要用带过期查询参数的下载 URL 充当身份，也不要把标题、发布时间或令牌拼进 ID。

#### 保存目录没有立即出现

这是预期行为。目录只在下载成功准备入库时创建，失败或排队中的任务不会提前创建空目录。

#### 缩略图 403，但主文件正常

缩略图不共享主文件请求头。改用可匿名访问的缩略图 URL，或暂不返回 `thumbnail_url`。

## 18. RSS/Atom 示例说明

仓库的 `examples/plugins/rss` 是一个完整示例，它展示了：

- `ValidateConfig` 校验 HTTP/HTTPS feed URL。
- `Fetch` 使用 `HTTPRequest` 获取 RSS/Atom。
- RSS `enclosure` 和 Atom `link rel="enclosure"` 转换为下载条目。
- 根据 URL 扩展名选择 `http` 或 `hls`。
- 解析 RFC 3339、RFC 1123Z、RFC 1123 发布时间。
- 返回保存目录和公开缩略图 URL。

它只声明 `network.public_http`，所以不会查询用户文件，也不会返回离线下载自定义头。开发需要认证的数据源插件时，可以在此基础上分别添加数据源请求头、`downloads.custom_headers` 权限和条目请求头。

示例使用仓库内本地 `replace` 便于联调。复制到独立仓库时，请把 import 和 `go.mod` 改为本文第 3 节所示的 SDK module 路径，并删除指向 MyObj 源码树的相对 `replace`。

## 19. 非 TinyGo 实现的底层 ABI 参考

官方开发路径是 TinyGo SDK。本节仅供实现其他 WASI 语言绑定时参考。

插件是 WASI Preview 1 command module，宿主通过 stdin/stdout 交换第 6 节 JSON。宿主模块名为 `myobj`，导出给插件的函数为：

```text
http_request(request_ptr: u32, request_len: u32, output_ptr: u32, output_cap: u32) -> i32
file_get(request_ptr: u32, request_len: u32, output_ptr: u32, output_cap: u32) -> i32
files_query(request_ptr: u32, request_len: u32, output_ptr: u32, output_cap: u32) -> i32
```

调用方把 UTF-8 JSON 写入线性内存的 request 区域，并提供 output 缓冲区。返回值非负时表示写入的 JSON 字节数；负值表示低层调用失败。业务错误通常仍以 `{"error":"..."}` JSON 返回。

`http_request` 的 body 字段是 `body_base64`；响应同样使用 `body_base64`。`file_get` 请求为 `{"uf_id":"..."}`；`files_query` 使用第 12 节过滤字段。

自行实现绑定仍必须满足 stdout 单一 JSON、执行时间、内存和权限限制。ABI v1 不承诺 Go SDK 内部细节，第三方绑定应针对真实 MyObj 版本运行兼容测试。

## 20. 安全最佳实践

- 只请求完成工作所需的最小权限。
- `Healthcheck` 和 `ValidateConfig` 保持纯本地、确定且快速。
- 不把令牌、Cookie、完整配置、带签名查询参数 URL 或文件列表写到 stdout、stderr 或 error。
- 解析远端 JSON/XML 时设置合理结构，不递归处理不可信的无限深数据。
- 为条目设计稳定 ID，并把短期凭据与身份分离。
- 使用 `request_headers` 传认证信息，避免把秘密放在 URL 查询参数。
- `header_hosts` 只列出确实需要认证的精确域名。
- 不根据远端响应拼接物理路径；`save_path` 始终是受限的用户虚拟路径。
- 把插件视为会被随时取消：不要依赖 finally 阶段提交远端事务。
- 对可选权限做降级；对必需权限返回明确错误。
- 发布前保存 `.myobj-plugin`、包 SHA-256、WASM SHA-256和对应源码标签，便于审计与复现。

## 21. 发布前检查清单

- [ ] `id` 稳定，`version` 高于已发布版本，`api_version` 为 `"1"`。
- [ ] manifest 为 UTF-8 无 BOM，未知字段和尾随内容已清除。
- [ ] 只声明实际需要的权限，并在 README 解释每项用途。
- [ ] 所有必填和类型规则都由 `ValidateConfig` 再次校验。
- [ ] `Healthcheck`、`ValidateConfig` 不调用宿主 API。
- [ ] `Fetch` 在 60 秒内完成，HTTP 调用次数保持合理。
- [ ] 返回条目不超过合理规模，stdout 明显小于 2 MiB。
- [ ] 每个条目有稳定 ID，签名 URL 更新不会改变 ID。
- [ ] HTTP/HLS、文件名、保存目录和发布时间均已验证。
- [ ] 自定义头没有危险头、CR/LF、重复名称或超限数据。
- [ ] HLS 的子列表、密钥、初始化分片和媒体分片域名均已核对。
- [ ] 缩略图为可匿名访问的公网 JPEG/PNG/WebP/GIF，且满足体积与像素限制。
- [ ] 文件查询使用最小过滤范围，分页保持过滤条件不变。
- [ ] 日志和错误中没有配置秘密、请求头值、完整查询参数或完整文件列表。
- [ ] `go test ./...` 通过。
- [ ] TinyGo `wasip1` 编译通过。
- [ ] `plugin pack` 和 `plugin validate` 通过。
- [ ] 在测试 MyObj 中完成安装、授权、手动运行、下载、凭据刷新和缩略图验证。

## 22. 相关源码与示例

- TinyGo SDK：`sdk/tinygo`
- RSS/Atom 示例：`examples/plugins/rss`
- manifest 与包校验：`src/pkg/plugin`
- 订阅执行与文件查询：`src/core/service/subscription_service.go`
- HTTP/HLS 请求头规则：`src/pkg/download/hls_headers.go`
- 虚拟路径规则：`src/pkg/virtualpath/path.go`

插件开发遇到文档与实际校验不一致时，以目标 MyObj 版本的 SDK、manifest 解析器和运行时代码为准，并在发布说明中标明已验证的 MyObj 版本。
