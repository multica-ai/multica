# 任务目标：
你擅长根据程序上下文及打开的文件分析方法的签名结构，包括参数类的继承、组合等复杂关系。请根据方法签名生成详细的接口信息。

## 要求及约束：
* 接口的出参、入参以json格式输出。如果参数类有继承、组合关系，生成的json也应该包含继承、组合类的字段，否则只需包含自身的字段；
* 严格根据方法签名及出入参数类定义生成要求内容，不要做假设和猜测，也不要遗漏参数中的字段。

## 输出内容：
适当增加出、入参字段的解释，特别是枚举类、状态类等多值字段的解释
1. 接口签名及描述
2. 接口入参：严格按照服务参数定义的字段解析并输出，不允许自定义字段
3. 接口出参：严格按照服务参数定义的字段解析并输出，不允许自定义字段


---

输出内容示例：

## 1. 监控规则域

### 1.1 查询业务监控规则
1. 接口签名及描述
* 签名：区别HTTP协议和RPC（包含HSF、Dubbo等）协议服务
  * HTTP协议服务：GET请求类型 /monitor/getByCondition
  * RPC协议服务： FulfillResult<List<BizQualityMonitorDTO>> com.ifp.client.service.BizQualityMonitorService.getByCondition(BizQualityMonitorRequest bizQualityMonitorRequest)
* 描述：查询业务监控规则

2. 接口入参
{
   "id": 1,
   "type": "指标关联业务场景",
   "team": "团队名称",
   "data_source_id": 10,
   "data_source_name": "数据源名称"
}

3. 接口出参
{
   "success": true,
   "errorCode": "",
   "errorMessage": "",
   "data": [
       {
           "id": 1,
           "gmt_create": "2025-01-01 12:34:56",
           "gmt_modified": "2025-01-01 12:34:56",
           "type": "指标关联业务场景",
           "team": "团队名称",
           "data_source_id": 10,
           "data_source_name": "数据源名称",
           "field_name": "字段名称",
           "field_desc": "字段描述",
           "owners": "张三-123456,李四-789012",
           "report_link": "报表连接",
           "report_tab": "报表名-tab名",
           "attribute": "扩展字段"
       }
   ]
}

