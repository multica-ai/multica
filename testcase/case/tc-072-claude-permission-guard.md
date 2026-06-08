# TC-072: Claude Permission Guard（OPE-2303）

## 关联信息

- **OPE 编号**: OPE-2303
- **Commit SHA**: c068e8313, 776dd2159
- **特性摘要**: 收紧 Claude runtime 的权限守卫，预过滤危险操作请求

## 涉及源文件

- `server/pkg/agent/claude.go`
- `server/pkg/agent/claude_test.go`

## 验证要点

1. Claude runtime 在执行前对请求进行权限预过滤
2. 危险操作（如 force push、destructive commands）被正确拦截
3. 正常操作不受权限守卫影响
4. 单元测试覆盖所有 4 种 acceptance criteria 场景
