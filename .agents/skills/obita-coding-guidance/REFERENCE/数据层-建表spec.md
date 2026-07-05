### 建表要求

1. 表、字段命名规范
    * 表名、字段名必须使用小写字母或数字，禁止出现数字开头，禁止两个下划线中间只出现数字。
      * 正例：getter_admin，task_config，level3_name
      * 反例：GetterAdmin，taskConfig，level_3_name
2. 索引规范
   * 主键索引： 无需指定索引名称，前缀关键字使用**PRIMARY KEY**
   * 唯一索引： uk_字段名（如 uk_user_id），前缀关键字使用**UNIQUE KEY**
   * 普通索引：idx_字段名（如 idx_user_status），前缀关键字使用**KEY**
3. 默认字段：字段id、gmt_create、gmt_modified默认添加

---

建表语句示例：

CREATE TABLE `batch_task` (
`id` bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '主键',
`gmt_create` datetime NOT NULL COMMENT '创建时间',
`gmt_modified` datetime NOT NULL COMMENT '修改时间',
`op_dep_no` varchar(64) DEFAULT NULL COMMENT '操作人工号',
`op_dep_nickname` varchar(64) DEFAULT NULL COMMENT '操作人花名',
`task_type` varchar(64) NOT NULL COMMENT '任务类型',
`task_name` varchar(64) NOT NULL COMMENT '任务名称',
`task_id` varchar(64) NOT NULL COMMENT '任务id',
`total_num` int(11) DEFAULT NULL COMMENT '任务总量',
`curr_num` int(11) DEFAULT NULL COMMENT '当前进度',
`status` varchar(64) NOT NULL COMMENT '任务状态',
`attribute` text COMMENT '扩展字段',
`env_type` varchar(16) NOT NULL COMMENT '环境(DAILY:日常 2:PRE ONLINE:生产 )',
PRIMARY KEY (`id`),
UNIQUE KEY `uk_task_id_env` (`task_id`,`env_type`),
KEY `idx_op_type_env` (`op_dep_no`,`task_type`,`env_type`)
) ENGINE=InnoDB AUTO_INCREMENT=471 DEFAULT CHARSET=utf8mb4 COMMENT='导入任务'
;
