# MyObj Python SDK

这个 SDK 封装了 MyObj 当前的 API Key + RSA 公钥认证协议，以及常用文件管理流程。

当前版本为 0.2.0。`upload_file()` 固定使用与服务端一致的 5MB 分片，已移除 `chunk_size` 参数；从 0.1.x 升级时，请删除调用代码中的该参数。

## 安装

在项目根目录执行：

```powershell
python -m pip install -e .\sdk\python
```

## 初始化

生成 API Key 时，请同时保存返回的 `key` 和 `public_key`。将公钥按 UTF-8 编码保存为 `public_key.pem`：

```python
from myobj_sdk import MyObjClient


client = MyObjClient(
    "http://localhost:8080",
    "替换成生成的 API Key",
    public_key_path="public_key.pem",
)
```

SDK 会为每次请求重新生成 `X-Timestamp`、`X-Nonce` 和 `X-Signature`，不要再额外设置 `Authorization` 请求头。

## Debug 日志

SDK 使用 Python 标准 `logging` 输出请求调试信息，logger 名称为 `myobj_sdk`。SDK 默认不会修改应用程序的日志级别或处理器，因此需要在创建客户端或发起请求前由调用方完成配置。

仅将 SDK 的 DEBUG 日志输出到控制台：

```python
import logging


sdk_logger = logging.getLogger("myobj_sdk")
sdk_logger.setLevel(logging.DEBUG)
sdk_logger.propagate = False

console_handler = logging.StreamHandler()
console_handler.setLevel(logging.DEBUG)
console_handler.setFormatter(
    logging.Formatter(
        "%(asctime)s %(levelname)s %(name)s %(message)s",
    )
)
sdk_logger.addHandler(console_handler)
```

同时写入 UTF-8 日志文件：

```python
file_handler = logging.FileHandler(
    "myobj-sdk.log",
    encoding="utf-8",
)
file_handler.setLevel(logging.DEBUG)
file_handler.setFormatter(
    logging.Formatter(
        "%(asctime)s %(levelname)s %(name)s %(message)s",
    )
)
sdk_logger.addHandler(file_handler)
```

如果应用已经统一配置了根 logger，也可以只设置 SDK 的日志级别：

```python
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logging.getLogger("myobj_sdk").setLevel(logging.DEBUG)
```

重复执行日志配置会重复添加处理器，建议只在应用入口配置一次。DEBUG 日志包含请求方法、地址、脱敏后的参数摘要、HTTP 状态码、业务状态码和耗时。API Key、认证签名、密码、令牌、文件内容及下载响应体不会写入日志。上传进度条开启时，控制台 DEBUG 日志会完整输出，进度条会在日志输出后自动恢复。

## 文件与目录

```python
# 文件列表
result = client.list_files(directory_id=1, page=1, page_size=20)
print(result["data"])

# 搜索文件；关键词和 Tag 至少提供一项，可限制在当前目录
# keyword 只匹配文件名，tag_ids 继续按 tag_mode 独立筛选
result = client.search_files("报告", directory_id=12, page=1, page_size=20)
tag_result = client.search_files(tag_ids=["标签ID一", "标签ID二"], tag_mode="all")

# 获取目录树
directories = client.get_directories()

# 创建、重命名和删除目录
client.create_directory(parent_id=1, name="资料")
directory_id = client.ensure_directory(parent_id=1, name="资料")
client.rename_directory(dir_id=12, new_dir_name="归档资料")
client.delete_directory(dir_id=12)

# 移动、重命名和删除文件
client.move_file(
    file_id="用户文件ID",
    target_directory_id=12,
)
client.rename_file("用户文件ID", "年度报告.pdf")
client.set_file_public("用户文件ID", True)
client.delete_files(["用户文件ID一", "用户文件ID二"])
```

接口使用的是文件列表返回的用户文件 ID（通常为 `uf_id`），不是底层存储文件 ID。

`ensure_directory()` 会按分页结果查找同名目录，未找到时创建后再次查询，因此目录不在第一页时也不会重复创建。可通过 `page_size` 调整每页数量。

## 文件标签

```python
# 读取标签、获取建议，并在失败时重新生成自动标签
tags = client.get_file_tags("用户文件ID")
suggestions = client.get_tag_suggestions("科幻", limit=20)
client.retry_file_tags("用户文件ID")

# 添加公开手工标签、屏蔽一个自动标签
client.update_manual_tags(
    "用户文件ID",
    add=[{"name": "收藏", "category_id": "other", "visibility": "public"}],
)
client.update_tag_exclusions("用户文件ID", suppress_tag_ids=["自动标签ID"])

# 最多对 100 个文件执行原子批量打标
client.batch_update_tags(
    ["用户文件ID一", "用户文件ID二"],
    add=[{"name": "待整理", "category_id": "other", "visibility": "private"}],
)

```

`list_files()`、`search_files()`、`search_public_files()` 和 `list_public_files()` 都支持 `tag_ids` 与 `tag_mode="all"|"any"`。手工标签默认私有，只有明确设为 `public` 才会随公开文件出现在文件广场。

## 上传

```python
result = client.upload_file(
    "D:/资料/报告.pdf",
    directory_id=12,
)
print(result["message"])
```

固定按 5MB 分片，并在终端显示上传进度条。SDK 会计算整文件和分片 MD5，支持服务端秒传及根据预检结果跳过已上传分片。默认在所有分片上传完成后立即返回；当服务端仍在处理时，返回结果中的 `data.status` 为 `processing`。开启 DEBUG 日志时，上传期间的控制台日志会完整输出，进度条会在日志输出后自动恢复。

需要等待服务端完成校验、存储和加密并返回最终文件结果时，显式开启等待：

```python
result = client.upload_file(
    "D:/资料/报告.pdf",
    directory_id=12,
    wait_for_completion=True,
    finalize_timeout=600,
)
```

等待期间从 `finalize_poll_interval` 开始按 1.5 倍退避，最大间隔由 `finalize_max_poll_interval` 控制，默认 5 秒；未设置 `finalize_timeout` 时仍会持续等待。

不需要终端进度条时可以关闭：

```python
client.upload_file(
    "D:/资料/报告.pdf",
    directory_id=12,
    show_progress=False,
)
```

已有的进度回调仍然可用。只需要自定义进度展示时，可以同时关闭默认进度条：

```python
def show_progress(completed: int, total: int) -> None:
    percent = 100 if total == 0 else completed * 100 // total
    print(f"上传进度：{percent}%")


client.upload_file(
    "D:/资料/报告.pdf",
    directory_id=12,
    progress=show_progress,
    show_progress=False,
)
```

加密上传：

```python
client.upload_file(
    "D:/资料/机密报告.pdf",
    directory_id=12,
    encrypted=True,
    file_password="文件解密密码",
)
```

## 下载

```python
client.download_file(
    "用户文件ID",
    "D:/下载/报告.pdf",
    progress=show_progress,
)

client.download_thumbnail(
    "用户文件ID",
    "D:/下载/报告-thumbnail.jpg",
)
```

`download_file` 会先创建异步准备任务，通过任务状态接口等待文件合并或解密完成，再请求一次文件并流式写入目标路径。轮询从 `poll_interval` 开始按 1.5 倍退避，最大间隔由 `max_poll_interval` 控制，默认 5 秒。下载期间先写入同目录下的 `.part` 文件，成功后再替换为最终文件。

## 修改缩略图

```python
client.update_thumbnail(
    "用户文件ID",
    "D:/封面/视频封面.jpg",
)
```

缩略图必须是 JPEG 图片，文件不超过 1MB，宽高均不超过 1000 像素。加密文件不支持修改缩略图。

## 打包下载

```python
package = client.create_package(
    ["用户文件ID一", "用户文件ID二"],
    package_name="项目资料.zip",
)
package_id = package["data"]["package_id"]

client.download_package(
    package_id,
    "D:/下载/项目资料.zip",
)
```

打包状态轮询同样从 `poll_interval` 开始按 1.5 倍退避，`max_poll_interval` 默认限制为 5 秒，`wait_timeout` 的既有超时语义不变。

## 异常处理

```python
from myobj_sdk import MyObjAPIError, MyObjHTTPError


try:
    client.list_files(page=1, page_size=20)
except MyObjAPIError as exc:
    print(exc.code, exc.message, exc.data)
except MyObjHTTPError as exc:
    print("网络或 HTTP 错误：", exc)
finally:
    client.close()
```

也可以使用上下文管理器自动关闭连接：

```python
from myobj_sdk import MyObjClient


with MyObjClient(
    "http://localhost:8080",
    "替换成生成的 API Key",
    public_key_path="public_key.pem",
) as client:
    print(client.list_files(page=1, page_size=20))
