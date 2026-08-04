-- MySQL 数据库初始化脚本
-- 包含建表语句和初始数据
-- 基于 SQLite 数据库结构和 clear_test_data.sql 脚本生成

-- 注意：执行此脚本前请确保已创建数据库
-- CREATE DATABASE IF NOT EXISTS myobj CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
-- USE myobj;

-- 开始事务
START TRANSACTION;

-- ================================
-- 1. 删除已存在的表（如果存在）
-- ================================
DROP TABLE IF EXISTS `group_power`;
DROP TABLE IF EXISTS `tag_rebuild_failure`;
DROP TABLE IF EXISTS `tag_rebuild_job`;
DROP TABLE IF EXISTS `tag_rule`;
DROP TABLE IF EXISTS `tag_rule_set`;
DROP TABLE IF EXISTS `file_metadata_state`;
DROP TABLE IF EXISTS `file_metadata`;
DROP TABLE IF EXISTS `user_file_tag_state`;
DROP TABLE IF EXISTS `user_file_tag_exclusion`;
DROP TABLE IF EXISTS `user_file_tag`;
DROP TABLE IF EXISTS `tag_definition`;
DROP TABLE IF EXISTS `tag_category`;
DROP TABLE IF EXISTS `power`;
DROP TABLE IF EXISTS `groups`;
DROP TABLE IF EXISTS `user_files`;
DROP TABLE IF EXISTS `file_chunk`;
DROP TABLE IF EXISTS `virtual_directory`;
DROP TABLE IF EXISTS `virtual_path`;
DROP TABLE IF EXISTS `schema_migration`;
DROP TABLE IF EXISTS `upload_chunk`;
DROP TABLE IF EXISTS `upload_task`;
DROP TABLE IF EXISTS `download_task`;
DROP TABLE IF EXISTS `plugin_audit_log`;
DROP TABLE IF EXISTS `subscription_item`;
DROP TABLE IF EXISTS `subscription_run`;
DROP TABLE IF EXISTS `subscription`;
DROP TABLE IF EXISTS `installed_plugin`;
DROP TABLE IF EXISTS `shares`;
DROP TABLE IF EXISTS `recycled`;
DROP TABLE IF EXISTS `disk`;
DROP TABLE IF EXISTS `sys_config`;
DROP TABLE IF EXISTS `api_key`;
DROP TABLE IF EXISTS `user_info`;
DROP TABLE IF EXISTS `file_info`;

-- ================================
-- 2. 创建基础权限表
-- ================================

-- 权限表
CREATE TABLE `power` (
    `id` INT NOT NULL AUTO_INCREMENT COMMENT '权限ID',
    `name` VARCHAR(255) NOT NULL COMMENT '权限名称',
    `description` TEXT NOT NULL COMMENT '权限描述',
    `characteristic` TEXT NOT NULL COMMENT '权限特征',
    `created_at` DATETIME NOT NULL COMMENT '创建时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_id` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='权限表';

-- 组表
CREATE TABLE `groups` (
    `id` INT NOT NULL AUTO_INCREMENT COMMENT '组ID',
    `name` VARCHAR(255) NOT NULL COMMENT '组名称',
    `created_at` DATETIME NOT NULL COMMENT '创建时间',
    `group_default` INT NOT NULL COMMENT '是否为默认组 0-否 1-是',
    `space` BIGINT DEFAULT NULL COMMENT '组默认可用存储空间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_id` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='组表';

-- 组权限关联表
CREATE TABLE `group_power` (
    `group_id` INT NOT NULL COMMENT '组ID',
    `power_id` INT NOT NULL COMMENT '权限ID',
    PRIMARY KEY (`group_id`, `power_id`),
    KEY `idx_power_id` (`power_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='组权限关联表';

-- ================================
-- 3. 创建用户相关表
-- ================================

-- 用户信息表
CREATE TABLE `user_info` (
    `id` VARCHAR(64) NOT NULL COMMENT '用户ID',
    `name` VARCHAR(255) NOT NULL COMMENT '用户昵称',
    `user_name` VARCHAR(255) NOT NULL COMMENT '用户名',
    `password` TEXT NOT NULL COMMENT '用户密码',
    `email` TEXT NOT NULL COMMENT '用户邮箱',
    `phone` VARCHAR(20) NOT NULL COMMENT '用户手机号',
    `group_id` INT NOT NULL COMMENT '用户组ID',
    `created_at` DATETIME NOT NULL COMMENT '创建时间',
    `space` BIGINT DEFAULT NULL COMMENT '用户可用存储空间',
    `file_password` TEXT DEFAULT NULL COMMENT '用户文件密码',
    `free_space` BIGINT DEFAULT NULL COMMENT '用户剩余存储空间',
    `state` INT NOT NULL DEFAULT 0 COMMENT '用户状态 0正常 1禁用',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_id` (`id`),
    KEY `idx_group_id` (`group_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户信息表';

-- API密钥表
CREATE TABLE `api_key` (
    `id` INT NOT NULL AUTO_INCREMENT COMMENT 'API密钥ID',
    `user_id` VARCHAR(64) NOT NULL COMMENT '用户ID',
    `key` TEXT NOT NULL COMMENT 'API密钥',
    `expires_at` DATETIME DEFAULT NULL COMMENT '过期时间',
    `created_at` DATETIME NOT NULL COMMENT '创建时间',
    `private_key` TEXT NOT NULL COMMENT '私钥',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_id` (`id`),
    KEY `idx_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='API密钥表';

-- ================================
-- 4. 创建文件相关表
-- ================================

-- 文件信息表
CREATE TABLE `file_info` (
    `id` VARCHAR(64) NOT NULL COMMENT '文件ID',
    `name` VARCHAR(255) NOT NULL COMMENT '文件原名',
    `random_name` VARCHAR(255) NOT NULL COMMENT '文件存储名（随机生成）',
    `size` BIGINT NOT NULL COMMENT '文件大小',
    `mime` VARCHAR(255) NOT NULL COMMENT '文件MIME类型',
    `thumbnail_img` TEXT DEFAULT NULL COMMENT '缩略图路径',
    `path` TEXT DEFAULT NULL COMMENT '文件实际存储路径',
    `file_hash` TEXT NOT NULL COMMENT '文件哈希值（全量hash）',
    `file_enc_hash` TEXT DEFAULT NULL COMMENT '加密文件哈希值',
    `chunk_signature` TEXT DEFAULT NULL COMMENT '分片签名（快速预检）',
    `first_chunk_hash` TEXT DEFAULT NULL COMMENT '第一个分片hash',
    `second_chunk_hash` TEXT DEFAULT NULL COMMENT '第二个分片hash',
    `third_chunk_hash` TEXT DEFAULT NULL COMMENT '第三个分片hash',
    `has_full_hash` BOOLEAN DEFAULT FALSE COMMENT '是否已计算全量hash',
    `is_enc` BOOLEAN DEFAULT FALSE COMMENT '是否加密',
    `is_chunk` BOOLEAN NOT NULL COMMENT '是否分块存储',
    `chunk_count` INT DEFAULT NULL COMMENT '分块数量',
    `enc_path` TEXT NOT NULL COMMENT '加密文件路径',
    `created_at` DATETIME DEFAULT NULL COMMENT '创建时间',
    `updated_at` DATETIME DEFAULT NULL COMMENT '更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_id` (`id`),
    KEY `idx_file_hash` (`file_hash`(255)),
    KEY `idx_chunk_signature` (`chunk_signature`(255)),
    KEY `idx_mime` (`mime`),
    KEY `idx_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='文件信息表';

-- 用户文件关联表
CREATE TABLE `user_files` (
    `user_id` VARCHAR(64) NOT NULL COMMENT '用户ID',
    `file_id` VARCHAR(64) NOT NULL COMMENT '文件ID',
    `file_name` TEXT NOT NULL COMMENT '文件名',
    `directory_id` INT NOT NULL COMMENT '虚拟目录ID',
    `public` BOOLEAN NOT NULL DEFAULT FALSE COMMENT '是否公开',
    `created_at` DATETIME NOT NULL COMMENT '创建时间',
    `deleted_at` DATETIME DEFAULT NULL COMMENT '删除时间',
    `uf_id` VARCHAR(64) NOT NULL COMMENT '用户文件ID',
    PRIMARY KEY (`uf_id`),
    KEY `idx_user_id` (`user_id`),
    KEY `idx_file_id` (`file_id`),
    KEY `idx_user_files_directory` (`user_id`, `directory_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户文件关联表';

-- 文件分片表
CREATE TABLE `file_chunk` (
    `id` VARCHAR(64) NOT NULL COMMENT '分片ID',
    `file_id` VARCHAR(64) NOT NULL COMMENT '文件ID',
    `chunk_path` TEXT NOT NULL COMMENT '分片文件路径',
    `chunk_size` BIGINT NOT NULL COMMENT '分片文件大小',
    `chunk_hash` TEXT NOT NULL COMMENT '分片文件哈希',
    `chunk_index` INT NOT NULL COMMENT '分片文件索引',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_id` (`id`),
    KEY `idx_file_id` (`file_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='文件分片表';

-- 虚拟目录表
CREATE TABLE `virtual_directory` (
    `id` INT NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `user_id` VARCHAR(64) NOT NULL COMMENT '用户ID',
    `name` VARCHAR(100) NOT NULL COMMENT '单级目录名称，根目录为空字符串',
    `parent_id` INT NOT NULL DEFAULT 0 COMMENT '父目录ID，根目录为0',
    `created_at` DATETIME NOT NULL COMMENT '创建时间',
    `updated_at` DATETIME NOT NULL COMMENT '更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_virtual_directory_sibling` (`user_id`, `parent_id`, `name`),
    KEY `idx_virtual_directory_parent` (`user_id`, `parent_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='虚拟目录表';

CREATE TABLE `schema_migration` (
    `version` VARCHAR(128) NOT NULL,
    `applied_at` DATETIME NOT NULL,
    PRIMARY KEY (`version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='数据库迁移版本';

-- ================================
-- 5. 创建上传下载任务表
-- ================================

-- 上传任务表
CREATE TABLE `upload_task` (
    `id` VARCHAR(64) NOT NULL COMMENT '任务ID',
    `user_id` VARCHAR(64) DEFAULT NULL COMMENT '用户ID',
    `file_name` TEXT NOT NULL COMMENT '文件名',
    `file_size` BIGINT NOT NULL COMMENT '文件大小（字节）',
    `chunk_size` BIGINT NOT NULL DEFAULT 5242880 COMMENT '分片大小（字节，默认5MB）',
    `total_chunks` INT NOT NULL COMMENT '总分片数',
    `uploaded_chunks` INT DEFAULT 0 COMMENT '已上传分片数',
    `chunk_signature` TEXT DEFAULT NULL COMMENT '文件hash签名（用于秒传检测）',
    `directory_id` INT NOT NULL COMMENT '目录ID',
    `temp_dir` TEXT DEFAULT NULL COMMENT '临时目录路径',
    `disk_id` TEXT DEFAULT NULL COMMENT '预检阶段选中的磁盘ID',
    `is_enc` BOOLEAN DEFAULT FALSE COMMENT '是否为加密上传',
    `first_chunk_hash` TEXT DEFAULT NULL COMMENT '第一分片哈希',
    `second_chunk_hash` TEXT DEFAULT NULL COMMENT '第二分片哈希',
    `third_chunk_hash` TEXT DEFAULT NULL COMMENT '第三分片哈希',
    `status` VARCHAR(20) DEFAULT 'pending' COMMENT '任务状态（pending/uploading/processing/completed/failed/aborted）',
    `processing_stage` VARCHAR(20) DEFAULT NULL COMMENT '后台处理阶段',
    `result_file_id` VARCHAR(64) DEFAULT NULL COMMENT '处理完成后的文件ID',
    `error_message` TEXT DEFAULT NULL COMMENT '错误信息',
    `create_time` DATETIME DEFAULT NULL COMMENT '创建时间',
    `update_time` DATETIME DEFAULT NULL COMMENT '更新时间',
    `expire_time` DATETIME DEFAULT NULL COMMENT '过期时间（7天后自动清理）',
    PRIMARY KEY (`id`),
    KEY `idx_user_id` (`user_id`),
    KEY `idx_upload_task_directory` (`user_id`, `directory_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='上传任务表（支持断点续传）';

-- 上传分片表
CREATE TABLE `upload_chunk` (
    `chunk_id` INT NOT NULL AUTO_INCREMENT COMMENT '分片ID',
    `user_id` VARCHAR(64) NOT NULL COMMENT '用户ID',
    `file_name` TEXT NOT NULL COMMENT '文件名',
    `file_size` INT DEFAULT NULL COMMENT '文件大小',
    `md5` TEXT DEFAULT NULL COMMENT 'MD5',
    `directory_id` INT NOT NULL COMMENT '目录ID',
    PRIMARY KEY (`chunk_id`),
    KEY `idx_user_id` (`user_id`),
    KEY `idx_upload_chunk_directory` (`user_id`, `directory_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='上传分片表';

-- 下载任务表
CREATE TABLE `download_task` (
    `id` VARCHAR(64) NOT NULL COMMENT '任务ID',
    `user_id` VARCHAR(64) DEFAULT NULL COMMENT '用户ID',
    `file_id` VARCHAR(64) DEFAULT NULL COMMENT '文件ID',
    `file_name` TEXT DEFAULT NULL COMMENT '文件名',
    `file_size` BIGINT DEFAULT NULL COMMENT '文件大小',
    `downloaded_size` BIGINT DEFAULT 0 COMMENT '已下载大小',
    `progress` INT DEFAULT 0 COMMENT '下载进度 (0-100)',
    `speed` BIGINT DEFAULT 0 COMMENT '下载速度 (字节/秒)',
    `type` INT NOT NULL COMMENT '任务类型',
    `url` TEXT DEFAULT NULL COMMENT '下载URL',
    `path` TEXT DEFAULT NULL COMMENT '下载路径',
    `save_path` TEXT DEFAULT NULL COMMENT '用户虚拟绝对保存路径',
    `state` INT DEFAULT NULL COMMENT '任务状态',
    `error_msg` TEXT DEFAULT NULL COMMENT '错误信息',
    `target_dir` TEXT DEFAULT NULL COMMENT '目标临时目录',
    `support_range` BOOLEAN DEFAULT FALSE COMMENT '是否支持断点续传',
    `enable_encryption` BOOLEAN DEFAULT FALSE COMMENT '是否加密存储',
    `info_hash` TEXT DEFAULT NULL COMMENT '种子InfoHash（BT/磁力链任务）',
    `file_index` INT DEFAULT NULL COMMENT '种子内文件索引（BT/磁力链任务）',
    `torrent_name` TEXT DEFAULT NULL COMMENT '种子名称（BT/磁力链任务）',
	`batch_id` VARCHAR(64) DEFAULT NULL COMMENT '下载批次ID',
	`run_token` VARCHAR(64) DEFAULT NULL COMMENT '本次执行令牌',
	`worker_id` VARCHAR(128) DEFAULT NULL COMMENT '当前工作进程ID',
	`lease_expires_at` DATETIME DEFAULT NULL COMMENT '任务租约到期时间',
	`retry_count` INT DEFAULT 0 COMMENT '已重试次数',
	`next_retry_at` DATETIME DEFAULT NULL COMMENT '下次允许重试时间',
	`reserved_size` BIGINT DEFAULT 0 COMMENT '已预留用户空间',
	`request_headers_encrypted` TEXT DEFAULT NULL COMMENT 'HTTP/HLS请求头密文',
	`header_hosts_json` TEXT DEFAULT NULL COMMENT '请求头精确主机白名单',
	`requires_headers` BOOLEAN DEFAULT FALSE COMMENT '是否等待更新请求头',
    `create_time` DATETIME DEFAULT NULL COMMENT '创建时间',
    `update_time` DATETIME DEFAULT NULL COMMENT '更新时间',
    `finish_time` DATETIME DEFAULT NULL COMMENT '完成时间',
    PRIMARY KEY (`id`),
    KEY `idx_user_id` (`user_id`),
    KEY `idx_info_hash` (`info_hash`(255)),
	KEY `idx_download_batch_id` (`batch_id`),
	KEY `idx_download_run_token` (`run_token`),
	KEY `idx_download_lease_expires` (`lease_expires_at`),
	KEY `idx_download_next_retry` (`next_retry_at`),
	KEY `idx_download_user_type_state_create` (`user_id`, `type`, `state`, `create_time`),
	KEY `idx_download_schedule` (`state`, `type`, `next_retry_at`, `create_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='下载任务表';

-- 可安装插件表
CREATE TABLE `installed_plugin` (
    `id` VARCHAR(128) NOT NULL,
    `name` VARCHAR(255) NOT NULL,
    `version` VARCHAR(64) NOT NULL,
    `api_version` VARCHAR(32) NOT NULL,
    `author` VARCHAR(255) DEFAULT NULL,
    `description` TEXT DEFAULT NULL,
    `manifest_json` TEXT NOT NULL,
    `package_path` TEXT NOT NULL,
    `wasm_path` TEXT NOT NULL,
    `package_sha256` VARCHAR(64) NOT NULL,
    `wasm_sha256` VARCHAR(64) NOT NULL,
    `permissions` TEXT DEFAULT NULL,
    `enabled` BOOLEAN NOT NULL DEFAULT TRUE,
    `installed_by` VARCHAR(64) DEFAULT NULL,
    `created_at` DATETIME NOT NULL,
    `updated_at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_installed_plugin_enabled` (`enabled`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='可安装WASM插件';

-- 用户订阅表
CREATE TABLE `subscription` (
    `id` VARCHAR(64) NOT NULL,
    `user_id` VARCHAR(64) NOT NULL,
    `name` VARCHAR(255) NOT NULL,
    `plugin_id` VARCHAR(128) NOT NULL,
    `plugin_version` VARCHAR(64) NOT NULL,
    `config_encrypted` TEXT,
    `granted_permissions` TEXT,
    `schedule_time` VARCHAR(5) NOT NULL,
    `save_path` TEXT NOT NULL,
    `initial_limit` INT NOT NULL DEFAULT 10,
    `max_items_per_run` INT NOT NULL DEFAULT 100,
    `source_generation` INT NOT NULL DEFAULT 1,
    `enabled` BOOLEAN NOT NULL DEFAULT TRUE,
    `status` VARCHAR(32) NOT NULL DEFAULT 'ready',
    `last_error` TEXT,
    `next_run_at` DATETIME DEFAULT NULL,
    `last_run_at` DATETIME DEFAULT NULL,
    `created_at` DATETIME NOT NULL,
    `updated_at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_subscription_user` (`user_id`),
    KEY `idx_subscription_plugin_id` (`plugin_id`),
    KEY `idx_subscription_due` (`enabled`, `next_run_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='插件订阅';

CREATE TABLE `subscription_run` (
    `id` VARCHAR(64) NOT NULL,
    `subscription_id` VARCHAR(64) NOT NULL,
    `trigger` VARCHAR(16) NOT NULL,
    `status` VARCHAR(32) NOT NULL,
    `run_token` VARCHAR(64) DEFAULT NULL,
    `lease_expires_at` DATETIME DEFAULT NULL,
    `items_found` INT NOT NULL DEFAULT 0,
    `tasks_created` INT NOT NULL DEFAULT 0,
    `items_skipped` INT NOT NULL DEFAULT 0,
    `error_msg` TEXT,
    `started_at` DATETIME DEFAULT NULL,
    `finished_at` DATETIME DEFAULT NULL,
    `created_at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_subscription_run` (`subscription_id`),
    KEY `idx_subscription_run_status` (`status`),
    KEY `idx_subscription_run_token` (`run_token`),
    KEY `idx_subscription_run_lease` (`lease_expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='订阅执行记录';

CREATE TABLE `subscription_item` (
    `id` VARCHAR(64) NOT NULL,
    `subscription_id` VARCHAR(64) NOT NULL,
    `source_generation` INT NOT NULL,
    `item_key` VARCHAR(64) NOT NULL,
    `external_id` TEXT,
    `title` TEXT,
    `url` TEXT NOT NULL,
    `download_type` VARCHAR(16) NOT NULL,
    `file_name` TEXT,
    `save_path` TEXT NOT NULL,
    `thumbnail_url` TEXT,
    `request_headers_encrypted` TEXT,
    `header_hosts_json` TEXT,
    `headers_digest` VARCHAR(64),
    `download_task_id` VARCHAR(64),
    `status` VARCHAR(32) NOT NULL,
    `error_msg` TEXT,
    `thumbnail_status` VARCHAR(32) NOT NULL DEFAULT 'none',
    `thumbnail_retry_count` INT NOT NULL DEFAULT 0,
    `thumbnail_next_retry_at` DATETIME DEFAULT NULL,
    `thumbnail_error` TEXT,
    `published_at` DATETIME DEFAULT NULL,
    `created_at` DATETIME NOT NULL,
    `updated_at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_subscription_item` (`subscription_id`, `source_generation`, `item_key`),
    KEY `idx_subscription_item_subscription` (`subscription_id`),
    KEY `idx_subscription_item_task` (`download_task_id`),
    KEY `idx_subscription_item_status` (`status`),
    KEY `idx_subscription_thumbnail_status` (`thumbnail_status`),
    KEY `idx_subscription_thumbnail_retry` (`thumbnail_next_retry_at`),
    KEY `idx_subscription_item_published` (`published_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='订阅条目';

CREATE TABLE `plugin_audit_log` (
    `id` VARCHAR(64) NOT NULL,
    `plugin_id` VARCHAR(128) NOT NULL,
    `plugin_version` VARCHAR(64),
    `subscription_id` VARCHAR(64),
    `user_id` VARCHAR(64),
    `action` VARCHAR(64) NOT NULL,
    `summary` TEXT,
    `result_count` INT NOT NULL DEFAULT 0,
    `duration_ms` BIGINT NOT NULL DEFAULT 0,
    `status` VARCHAR(32) NOT NULL,
    `error_msg` TEXT,
    `created_at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_plugin_audit_plugin` (`plugin_id`),
    KEY `idx_plugin_audit_subscription` (`subscription_id`),
    KEY `idx_plugin_audit_user` (`user_id`),
    KEY `idx_plugin_audit_created` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='插件审计日志';

-- ================================
-- 6. 创建分享和回收站表
-- ================================

-- 分享表
CREATE TABLE `shares` (
    `id` INT NOT NULL AUTO_INCREMENT COMMENT '分享记录ID',
    `user_id` VARCHAR(64) NOT NULL COMMENT '用户ID',
    `file_id` VARCHAR(64) NOT NULL COMMENT '文件ID',
    `token` TEXT NOT NULL COMMENT '分享令牌',
    `expires_at` DATETIME NOT NULL COMMENT '分享过期时间',
    `password_hash` TEXT NOT NULL COMMENT '访问密码哈希',
    `download_count` INT NOT NULL DEFAULT 0 COMMENT '下载次数统计',
    `created_at` DATETIME NOT NULL COMMENT '分享创建时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_id` (`id`),
    KEY `idx_user_id` (`user_id`),
    KEY `idx_file_id` (`file_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='分享表';

-- 回收站表
CREATE TABLE `recycled` (
    `id` VARCHAR(64) NOT NULL COMMENT '回收站ID',
    `file_id` VARCHAR(64) NOT NULL COMMENT '文件ID',
    `item_type` VARCHAR(16) NOT NULL DEFAULT 'file' COMMENT '条目类型：file/folder',
    `item_name` TEXT NULL COMMENT '显示名称',
    `original_parent_id` INT NOT NULL DEFAULT 0 COMMENT '原父目录ID',
    `total_size` BIGINT NOT NULL DEFAULT 0 COMMENT '汇总大小',
    `item_count` INT NOT NULL DEFAULT 1 COMMENT '汇总项目数',
    `user_id` VARCHAR(64) NOT NULL COMMENT '用户ID',
    `created_at` DATETIME NOT NULL COMMENT '删除时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_id` (`id`),
    KEY `idx_user_id` (`user_id`),
    KEY `idx_file_id` (`file_id`),
    KEY `idx_recycled_user_type_created` (`user_id`, `item_type`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='回收站表';

CREATE TABLE `recycled_directory_node` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `recycled_id` VARCHAR(64) NOT NULL,
    `original_dir_id` INT NOT NULL,
    `parent_original_id` INT NOT NULL DEFAULT 0,
    `name` TEXT NOT NULL,
    `depth` INT NOT NULL DEFAULT 0,
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_recycled_original_dir` (`recycled_id`, `original_dir_id`),
    KEY `idx_recycled_node_parent` (`parent_original_id`),
    KEY `idx_recycled_node_depth` (`depth`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='回收站目录节点';

CREATE TABLE `recycled_directory_file` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `recycled_id` VARCHAR(64) NOT NULL,
    `file_id` VARCHAR(64) NOT NULL,
    `original_dir_id` INT NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_recycled_directory_file` (`recycled_id`, `file_id`),
    KEY `idx_recycled_file_dir` (`original_dir_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='回收站目录文件成员';

-- ================================
-- 7. 创建磁盘和系统配置表
-- ================================

-- 磁盘表
CREATE TABLE `disk` (
    `id` VARCHAR(64) NOT NULL COMMENT '磁盘ID',
    `size` INT NOT NULL COMMENT '磁盘总大小',
    `disk_path` TEXT NOT NULL COMMENT '磁盘路径',
    `data_path` TEXT NOT NULL COMMENT '数据存储路径',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_id` (`id`),
    KEY `idx_disk_path` (`disk_path`(255))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='磁盘信息表';

-- 文件标签分类
CREATE TABLE `tag_category` (
    `id` VARCHAR(64) NOT NULL,
    `code` VARCHAR(64) NOT NULL,
    `name` VARCHAR(64) NOT NULL,
    `color` VARCHAR(32) NOT NULL,
    `sort_order` INT NOT NULL DEFAULT 0,
    `enabled` BOOLEAN NOT NULL DEFAULT TRUE,
    `builtin` BOOLEAN NOT NULL DEFAULT FALSE,
    `created_at` DATETIME NOT NULL,
    `updated_at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_tag_category_code` (`code`),
    KEY `idx_tag_category_enabled` (`enabled`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='文件标签分类';

CREATE TABLE `tag_definition` (
    `id` VARCHAR(64) NOT NULL,
    `name` VARCHAR(255) NOT NULL,
    `normalized_name` VARCHAR(191) NOT NULL,
    `category_id` VARCHAR(64) NOT NULL,
    `created_at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_tag_definition` (`normalized_name`, `category_id`),
    KEY `idx_tag_definition_category` (`category_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='标签定义';

CREATE TABLE `user_file_tag` (
    `id` VARCHAR(64) NOT NULL,
    `user_id` VARCHAR(64) NOT NULL,
    `uf_id` VARCHAR(64) NOT NULL,
    `tag_id` VARCHAR(64) NOT NULL,
    `source_type` VARCHAR(32) NOT NULL,
    `source_key` VARCHAR(128) NOT NULL DEFAULT '',
    `rule_version` BIGINT NOT NULL DEFAULT 0,
    `visibility` VARCHAR(16) NOT NULL DEFAULT 'inherit',
    `created_by` VARCHAR(64) NOT NULL DEFAULT '',
    `created_at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_user_file_tag_source` (`uf_id`, `tag_id`, `source_type`, `source_key`),
    KEY `idx_user_tag_file` (`user_id`, `tag_id`, `uf_id`),
    KEY `idx_user_file_tag` (`user_id`, `uf_id`),
    KEY `idx_uf_source` (`uf_id`, `source_type`),
    KEY `idx_user_file_tag_visibility` (`visibility`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户文件标签来源';

CREATE TABLE `user_file_tag_exclusion` (
    `user_id` VARCHAR(64) NOT NULL,
    `uf_id` VARCHAR(64) NOT NULL,
    `tag_id` VARCHAR(64) NOT NULL,
    `created_at` DATETIME NOT NULL,
    PRIMARY KEY (`user_id`, `uf_id`, `tag_id`),
    KEY `idx_tag_exclusion_uf` (`uf_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户屏蔽的自动标签';

CREATE TABLE `user_file_tag_state` (
    `uf_id` VARCHAR(64) NOT NULL,
    `user_id` VARCHAR(64) NOT NULL,
    `global_version` BIGINT NOT NULL DEFAULT 0,
    `user_version` BIGINT NOT NULL DEFAULT 0,
    `metadata_version` BIGINT NOT NULL DEFAULT 0,
    `status` VARCHAR(32) NOT NULL,
    `last_error` TEXT,
    `retry_count` INT NOT NULL DEFAULT 0,
    `next_retry_at` DATETIME NULL,
    `run_token` VARCHAR(64) NOT NULL DEFAULT '',
    `lease_expires_at` DATETIME NULL,
    `generated_at` DATETIME NULL,
    `updated_at` DATETIME NOT NULL,
    PRIMARY KEY (`uf_id`),
    KEY `idx_tag_state_user` (`user_id`),
    KEY `idx_tag_state_global_version` (`global_version`),
    KEY `idx_tag_state_schedule` (`status`, `next_retry_at`),
    KEY `idx_tag_state_run_token` (`run_token`),
    KEY `idx_tag_state_lease` (`lease_expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='自动标签生成状态和队列';

CREATE TABLE `file_metadata` (
    `id` VARCHAR(64) NOT NULL,
    `file_id` VARCHAR(64) NOT NULL,
    `provider` VARCHAR(64) NOT NULL,
    `key_name` VARCHAR(128) NOT NULL,
    `value` TEXT NOT NULL,
    `value_type` VARCHAR(16) NOT NULL DEFAULT 'string',
    `version` BIGINT NOT NULL DEFAULT 1,
    `updated_at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_file_metadata` (`file_id`, `provider`, `key_name`),
    KEY `idx_file_metadata_file` (`file_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='物理文件可扩展元数据';

CREATE TABLE `file_metadata_state` (
    `file_id` VARCHAR(64) NOT NULL,
    `version` BIGINT NOT NULL DEFAULT 0,
    `status` VARCHAR(32) NOT NULL,
    `last_error` TEXT,
    `retry_count` INT NOT NULL DEFAULT 0,
    `next_retry_at` DATETIME NULL,
    `run_token` VARCHAR(64) NOT NULL DEFAULT '',
    `lease_expires_at` DATETIME NULL,
    `updated_at` DATETIME NOT NULL,
    PRIMARY KEY (`file_id`),
    KEY `idx_metadata_state_status` (`status`),
    KEY `idx_metadata_state_retry` (`next_retry_at`),
    KEY `idx_metadata_state_lease` (`lease_expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='物理文件元数据提取状态';

CREATE TABLE `tag_rule_set` (
    `id` VARCHAR(64) NOT NULL,
    `scope_type` VARCHAR(16) NOT NULL,
    `scope_id` VARCHAR(64) NOT NULL DEFAULT '',
    `version` BIGINT NOT NULL,
    `revision` INT NOT NULL DEFAULT 1,
    `status` VARCHAR(16) NOT NULL,
    `based_on_version` BIGINT NOT NULL DEFAULT 0,
    `created_by` VARCHAR(64) NOT NULL DEFAULT '',
    `created_at` DATETIME NOT NULL,
    `updated_at` DATETIME NOT NULL,
    `published_at` DATETIME NULL,
    PRIMARY KEY (`id`),
    KEY `idx_tag_rule_scope` (`scope_type`, `scope_id`, `status`, `version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='标签规则集版本';

CREATE TABLE `tag_rule` (
    `id` VARCHAR(64) NOT NULL,
    `rule_set_id` VARCHAR(64) NOT NULL,
    `rule_type` VARCHAR(32) NOT NULL,
    `target_field` VARCHAR(128) NOT NULL DEFAULT 'filename',
    `pattern` TEXT NOT NULL,
    `replacement` TEXT,
    `category_id` VARCHAR(64) NOT NULL DEFAULT 'other',
    `priority` INT NOT NULL DEFAULT 0,
    `weight` DECIMAL(8,3) NOT NULL DEFAULT 1,
    `enabled` BOOLEAN NOT NULL DEFAULT TRUE,
    `created_at` DATETIME NOT NULL,
    `updated_at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_tag_rule_set` (`rule_set_id`),
    KEY `idx_tag_rule_type` (`rule_type`),
    KEY `idx_tag_rule_category` (`category_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='标签分词和提取规则';

CREATE TABLE `tag_rebuild_job` (
    `id` VARCHAR(64) NOT NULL,
    `scope_type` VARCHAR(16) NOT NULL,
    `scope_id` VARCHAR(64) NOT NULL DEFAULT '',
    `target_version` BIGINT NOT NULL,
    `status` VARCHAR(32) NOT NULL,
    `cursor_value` VARCHAR(64) NOT NULL DEFAULT '',
    `total` BIGINT NOT NULL DEFAULT 0,
    `processed` BIGINT NOT NULL DEFAULT 0,
    `succeeded` BIGINT NOT NULL DEFAULT 0,
    `failed` BIGINT NOT NULL DEFAULT 0,
    `last_error` TEXT,
    `run_token` VARCHAR(64) NOT NULL DEFAULT '',
    `lease_expires_at` DATETIME NULL,
    `requested_by` VARCHAR(64) NOT NULL DEFAULT '',
    `started_at` DATETIME NULL,
    `finished_at` DATETIME NULL,
    `created_at` DATETIME NOT NULL,
    `updated_at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_tag_rebuild_scope` (`scope_type`, `scope_id`),
    KEY `idx_tag_job_schedule` (`status`, `lease_expires_at`),
    KEY `idx_tag_job_run_token` (`run_token`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='标签全量重建任务';

CREATE TABLE `tag_rebuild_failure` (
    `job_id` VARCHAR(64) NOT NULL,
    `uf_id` VARCHAR(64) NOT NULL,
    `user_id` VARCHAR(64) NOT NULL,
    `status` VARCHAR(32) NOT NULL,
    `error_message` TEXT,
    `retry_count` INT NOT NULL DEFAULT 0,
    `created_at` DATETIME NOT NULL,
    `updated_at` DATETIME NOT NULL,
    PRIMARY KEY (`job_id`, `uf_id`),
    KEY `idx_tag_rebuild_failure_status` (`job_id`, `status`),
    KEY `idx_tag_rebuild_failure_uf` (`uf_id`),
    KEY `idx_tag_rebuild_failure_user` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='标签重建逐文件失败明细';

-- 系统配置表
CREATE TABLE `sys_config` (
    `id` INT NOT NULL AUTO_INCREMENT COMMENT '配置ID',
    `key` VARCHAR(255) NOT NULL COMMENT '配置键',
    `value` TEXT NOT NULL COMMENT '配置值',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_id` (`id`),
    KEY `idx_key` (`key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='系统配置表';

-- ================================
-- 8. 插入初始数据
-- ================================

-- 插入组数据
INSERT INTO `groups` (`id`, `name`, `created_at`, `group_default`, `space`) VALUES
(1, 'administer', '2025-11-10 23:04:08', 0, NULL),
(2, 'user', '2025-11-15 23:23:29', 1, 500);

INSERT INTO `tag_category` (`id`, `code`, `name`, `color`, `sort_order`, `enabled`, `builtin`, `created_at`, `updated_at`) VALUES
('title', 'title', '标题', '#409eff', 10, TRUE, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('file_type', 'file_type', '文件类型', '#67c23a', 20, TRUE, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('year', 'year', '年份', '#e6a23c', 30, TRUE, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('season_episode', 'season_episode', '季集', '#f56c6c', 40, TRUE, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('resolution', 'resolution', '分辨率', '#909399', 50, TRUE, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('codec', 'codec', '编码', '#7b61ff', 60, TRUE, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('source', 'source', '来源', '#13ce66', 70, TRUE, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('language', 'language', '语言', '#ff8a00', 80, TRUE, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('other', 'other', '其他', '#909399', 90, TRUE, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00');

INSERT INTO `tag_rule_set` (`id`, `scope_type`, `scope_id`, `version`, `revision`, `status`, `based_on_version`, `created_by`, `created_at`, `updated_at`, `published_at`) VALUES
('global-tag-rules-v1', 'global', '', 1, 1, 'active', 0, 'system', '2026-08-04 00:00:00', '2026-08-04 00:00:00', '2026-08-04 00:00:00');

INSERT INTO `tag_rule` (`id`, `rule_set_id`, `rule_type`, `target_field`, `pattern`, `replacement`, `category_id`, `priority`, `weight`, `enabled`, `created_at`, `updated_at`) VALUES
('global-rule-year-v1', 'global-tag-rules-v1', 'regex', 'basename', '(?:^|[^0-9])((?:19|20)\\d{2})(?:[^0-9]|$)', '$1', 'year', 90, 1, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('global-rule-resolution-v1', 'global-tag-rules-v1', 'regex', 'basename', '(?i)(?:^|[^a-z0-9])(2160p|4k|1080p|720p|8k)(?:[^a-z0-9]|$)', '$1', 'resolution', 100, 1, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('global-rule-codec-v1', 'global-tag-rules-v1', 'regex', 'basename', '(?i)(?:^|[^a-z0-9])(h\\.?264|h\\.?265|x264|x265|hevc|av1)(?:[^a-z0-9]|$)', '$1', 'codec', 95, 1, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('global-rule-source-v1', 'global-tag-rules-v1', 'regex', 'basename', '(?i)(?:^|[^a-z0-9])(web[- .]?dl|web[- .]?rip|blu[- .]?ray|bdrip|hdtv)(?:[^a-z0-9]|$)', '$1', 'source', 90, 1, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('global-rule-episode-v1', 'global-tag-rules-v1', 'regex', 'basename', '(?i)(?:^|[^a-z0-9])(s\\d{1,2}e\\d{1,3})(?:[^a-z0-9]|$)', '$1', 'season_episode', 100, 1, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00'),
('global-rule-language-v1', 'global-tag-rules-v1', 'regex', 'basename', '(国语|粤语|日语|英语|中文字幕|中英字幕)', '$1', 'language', 90, 1, TRUE, '2026-08-04 00:00:00', '2026-08-04 00:00:00');

INSERT INTO `sys_config` (`key`, `value`) VALUES
('auto_tag_enabled', 'true'),
('auto_tag_limit', '20');

-- 插入权限数据
INSERT INTO `power` (`id`, `name`, `description`, `created_at`, `characteristic`) VALUES
(1, '用户查看', '查看系统所有用户', '2025-11-09 22:35:22', 'user:get'),
(2, '用户修改', '修改系统用户信息', '2025-11-09 22:35:50', 'user:update'),
(3, '用户删除', '删除系统用户', '2025-11-09 22:36:07', 'user:delete'),
(4, '用户停用', '暂停用户所有功能', '2025-11-09 22:36:26', 'user:state'),
(5, '用户空间分配', '分配用户可用空间大小', '2025-11-09 22:36:58', 'user:space'),
(6, '挂载磁盘', '挂载系统可用磁盘', '2025-11-09 23:35:06', 'disk:mount'),
(7, '删除挂载磁盘', '删除已经挂载的磁盘', '2025-11-10 00:27:35', 'disk:delete'),
(8, '查看挂载磁盘', '查看已经挂载磁盘的信息', '2025-11-10 00:27:59', 'disk:get'),
(9, '上传文件', '上传文件到磁盘', '2025-11-10 23:08:13', 'file:upload'),
(10, '重命名文件', '重命名磁盘文件', '2025-11-10 23:08:28', 'file:rechristen'),
(11, '分享文件', '创建文件分享链接', '2025-11-10 23:08:47', 'file:share'),
(12, '下载文件', '下载磁盘中的文件', '2025-11-10 23:11:02', 'file:download'),
(13, '离线下载', '离线下载文件到磁盘', '2025-11-10 23:13:30', 'file:offLine'),
(14, '文件保险箱', '加密文件的上传修改下载', '2025-11-10 23:15:34', 'file:insurance'),
(15, '文件预览', '查看文件和预览支持格式的文件', '2025-11-10 23:15:48', 'file:preview'),
(16, '创建目录', '创建文件目录', '2025-11-10 23:16:34', 'dir:create'),
(17, '删除目录', '删除已经存在的目录', '2025-11-10 23:16:48', 'dir:delete'),
(18, '创建apikey', '创建当前用户权限的apikey', '2025-11-10 23:18:35', 'apikey:create'),
(19, '删除apikey', '删除当前用户已存在的apikey', '2025-11-10 23:57:52', 'apikey:delete'),
(20, '修改其他用户信息', '修改其他用户信息，包括密码', '2025-11-12 20:52:19', 'user:update:else'),
(21, '用户密码修改', '修改用户自身密码', '2025-11-13 01:23:28', 'user:update:password'),
(22, '用户文件密码', '设置，修改文件密码', '2025-11-13 19:14:46', 'file:update:filePassword'),
(23, '移动文件/目录', '移动文件或目录至其他虚拟目录', '2025-11-18 01:17:59', 'file:move'),
(24, '删除文件', '删除文件（移动到回收站）', '2025-12-11 19:02:02', 'file:delete'),
(25, 'WebDAV访问', '允许通过WebDAV协议访问文件系统', '2025-12-30 07:34:05', 'webdav:access'),
(26, '文件标签', '维护文件标签和个人分词词典', '2026-08-04 00:00:00', 'file:tag');

-- 插入组权限关联数据
INSERT INTO `group_power` (`group_id`, `power_id`) VALUES
(1, 1),
(1, 2),
(1, 3),
(1, 4),
(1, 5),
(1, 6),
(1, 7),
(1, 8),
(1, 9),
(1, 10),
(1, 11),
(1, 12),
(1, 13),
(1, 14),
(1, 15),
(1, 16),
(1, 17),
(1, 18),
(1, 19),
(1, 20),
(1, 21),
(1, 22),
(1, 23),
(1, 24),
(1, 25),
(1, 26),
(2, 9),
(2, 10),
(2, 11),
(2, 12),
(2, 13),
(2, 14),
(2, 15),
(2, 16),
(2, 17),
(2, 18),
(2, 19),
(2, 22),
(2, 23),
(2, 24),
(2, 25),
(2, 26);

-- 提交事务
COMMIT;

-- ================================
-- 9. 验证初始数据
-- ================================
SELECT '=== 数据库初始化完成，以下是初始数据统计 ===' AS info;
SELECT 'groups 表记录数:' AS info, COUNT(*) AS count FROM `groups`;
SELECT 'power 表记录数:' AS info, COUNT(*) AS count FROM `power`;
SELECT 'group_power 表记录数:' AS info, COUNT(*) AS count FROM `group_power`;

SELECT '=== 所有表已创建 ===' AS info;
SHOW TABLES;
