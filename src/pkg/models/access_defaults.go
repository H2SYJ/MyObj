package models

const (
	DefaultAdminGroupID   = 1
	DefaultUserGroupID    = 2
	DefaultUserSpaceBytes = int64(500 * 1024 * 1024 * 1024)
)

type DefaultPowerDefinition struct {
	Name               string
	Description        string
	Characteristic     string
	GrantToDefaultUser bool
}

var DefaultPowerDefinitions = []DefaultPowerDefinition{
	{Name: "用户查看", Description: "查看系统所有用户", Characteristic: "user:get"},
	{Name: "用户修改", Description: "修改系统用户信息", Characteristic: "user:update", GrantToDefaultUser: true},
	{Name: "用户删除", Description: "删除系统用户", Characteristic: "user:delete"},
	{Name: "用户停用", Description: "暂停用户所有功能", Characteristic: "user:state"},
	{Name: "用户空间分配", Description: "分配用户可用空间大小", Characteristic: "user:space"},
	{Name: "修改其他用户信息", Description: "修改其他用户信息，包括密码", Characteristic: "user:update:else"},
	{Name: "用户密码修改", Description: "修改用户自身密码", Characteristic: "user:update:password", GrantToDefaultUser: true},
	{Name: "挂载磁盘", Description: "挂载系统可用磁盘", Characteristic: "disk:mount"},
	{Name: "删除挂载磁盘", Description: "删除已经挂载的磁盘", Characteristic: "disk:delete"},
	{Name: "查看挂载磁盘", Description: "查看已经挂载磁盘的信息", Characteristic: "disk:get"},
	{Name: "文件上传", Description: "上传文件到磁盘", Characteristic: "file:upload", GrantToDefaultUser: true},
	{Name: "重命名文件", Description: "重命名磁盘文件", Characteristic: "file:rechristen", GrantToDefaultUser: true},
	{Name: "分享文件", Description: "创建文件分享链接", Characteristic: "file:share", GrantToDefaultUser: true},
	{Name: "文件下载", Description: "下载磁盘中的文件", Characteristic: "file:download", GrantToDefaultUser: true},
	{Name: "离线下载", Description: "离线下载文件到磁盘", Characteristic: "file:offLine", GrantToDefaultUser: true},
	{Name: "文件保险箱", Description: "加密文件的上传修改下载", Characteristic: "file:insurance", GrantToDefaultUser: true},
	{Name: "文件预览", Description: "查看文件和预览支持格式的文件", Characteristic: "file:preview", GrantToDefaultUser: true},
	{Name: "文件编辑", Description: "在线编辑文本文件内容", Characteristic: "file:edit", GrantToDefaultUser: true},
	{Name: "文件标签", Description: "维护文件与目录标签", Characteristic: "file:tag", GrantToDefaultUser: true},
	{Name: "用户文件密码", Description: "设置，修改文件密码", Characteristic: "file:update:filePassword", GrantToDefaultUser: true},
	{Name: "移动文件/目录", Description: "移动文件或目录至其他虚拟目录", Characteristic: "file:move", GrantToDefaultUser: true},
	{Name: "删除文件", Description: "删除文件（移动到回收站）", Characteristic: "file:delete", GrantToDefaultUser: true},
	{Name: "创建目录", Description: "创建文件目录", Characteristic: "dir:create", GrantToDefaultUser: true},
	{Name: "删除目录", Description: "删除已经存在的目录", Characteristic: "dir:delete", GrantToDefaultUser: true},
	{Name: "创建apikey", Description: "创建当前用户权限的apikey", Characteristic: "apikey:create", GrantToDefaultUser: true},
	{Name: "删除apikey", Description: "删除当前用户已存在的apikey", Characteristic: "apikey:delete", GrantToDefaultUser: true},
	{Name: "WebDAV访问", Description: "允许通过WebDAV协议访问文件系统", Characteristic: "webdav:access", GrantToDefaultUser: true},
}
