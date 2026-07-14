# MyObj Python SDK

这个 SDK 封装了 MyObj 当前的 API Key + RSA 公钥认证协议，以及常用文件管理流程。

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

## 文件与目录

```python
# 文件列表
result = client.list_files(virtual_path="/", page=1, page_size=20)
print(result["data"])

# 搜索文件
result = client.search_files("报告", page=1, page_size=20)

# 获取目录树
paths = client.get_virtual_paths()

# 创建、重命名和删除目录
client.create_directory(parent_level="/", dir_path="资料")
path_id = client.ensure_directory(parent_level="2", dir_path="资料")
client.rename_directory(dir_id=12, new_dir_name="归档资料")
client.delete_directory(dir_id=12)

# 移动、重命名和删除文件
client.move_file(
    file_id="用户文件ID",
    source_path="/报告.pdf",
    target_path="/资料/报告.pdf",
)
client.rename_file("用户文件ID", "年度报告.pdf")
client.set_file_public("用户文件ID", True)
client.delete_files(["用户文件ID一", "用户文件ID二"])
```

接口使用的是文件列表返回的用户文件 ID（通常为 `uf_id`），不是底层存储文件 ID。

## 上传

```python
def show_progress(completed: int, total: int) -> None:
    percent = 100 if total == 0 else completed * 100 // total
    print(f"上传进度：{percent}%")


result = client.upload_file(
    "D:/资料/报告.pdf",
    path_id="目标目录ID",
    progress=show_progress,
)
print(result["message"])
```

默认按 5MB 分片。SDK 会计算整文件和分片 MD5，支持服务端秒传及根据预检结果跳过已上传分片。

加密上传：

```python
client.upload_file(
    "D:/资料/机密报告.pdf",
    path_id="目标目录ID",
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

`download_file` 会先创建异步准备任务，等待文件合并或解密完成，再流式写入目标文件。下载期间先写入同目录下的 `.part` 文件，成功后再替换为最终文件。

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
