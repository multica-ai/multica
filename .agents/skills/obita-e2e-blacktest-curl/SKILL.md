---
name: obita-e2e-blacktest-curl
description: "这个技能是帮助用户做前端接口的端到端联调，通过组装产品功能的CURL接口和参数，查看日志的方式。完成端到端的黑盒测试。"
---


# obita-e2e-blacktest-curl

* 由模型自己完成端到端的黑盒测试，不需要人工干预

* 执行过程中需要重启服务使修改生效，请执行`./start.sh` 脚本

## 测试前置准备

* 运行项目 `./start.sh` 脚本启动服务
 - 日志确认启动没有报错
 - 日志确认健康检查通过

* 运行登录脚本获取会话 Cookie，登录脚本

```bash
# 位置: scripts/login.sh

# 用法
./scripts/login.sh <username:admin> <password:obitaadmin> [backend_url]

# 示例
./scripts/login.sh admin your_password http://localhost:8080

# 备注
登录成功后 Cookie 会保存到 `/tmp/e2e-cookies.txt`，后续请求会自动使用该 Cookie。
```

## 系统日志配置文件
- obita-llm-backend日志目录: `obita-llm/obita-llm-backend/src/main/resources/logback-spring.xml`
- obita-llm-runtime日志目录: `obita-llm/obita-llm-runtime/backend-runtime-boot/src/main/resources/logback-spring.xml`

## 测试步骤

1、 查看需要联调模块的接口文档 `docs/system-design/frontend/interface`
2、 锁定需要联调的接口和用户确认范围，确认之后继续执行
3、 按用户在frontend项目中提供出的页面产品功能，逐一列出不同的参数组合，作为测试列表
4、 按每一条测试用例，发送请求并获得正确的返回数据，`将返回的数据打印在终端控制台`
5、 过程中必须通过查看日志的方式，确认obita-llm-backend和obita-llm-runtime中的代码执行正确，`截取日志并打印在控制台`
6、 通过接口和日志找出BUG，`将BUG问题打印在终端控制台`
7、 发现确定性的BUG，完成BUG修复。并回到步骤 `3` 确认修复成功
8、 发现不确定是BUG还是需求问题，需要和用户确认。再回到步骤 `3` 确认修复成功


## 执行规则

- 如果发现中间过程中的日志不足以定位问题，可通过临时增加日志的方式。** 临时调试日志在BUG修复完成后需要删除 **
- 如果模块的日志有问题，需要给出生产级系统日志方案，与用户确认后可执行。


## 完成报告
1、确认执行步骤循环执行了几次
2、确认修复的BUG清单，以及修复代码改动方案
3、确保所有接口和参数都执行成功
