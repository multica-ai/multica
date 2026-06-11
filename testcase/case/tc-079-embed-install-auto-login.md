# TC-079: embed 版 install.sh 自动 login 与同步检查（OPE-2279）

## 关联信息

- **OPE 编号**: OPE-2279
- **Gitee PR**: !369
- **Commit SHA**: 310094221
- **特性摘要**: 补齐 embed 版 install.sh 的自动 login 流程，并添加同步检查

## 涉及源文件

- `Makefile`
- `scripts/install.sh`
- `server/internal/handler/install_scripts/install.sh`

## 验证要点

1. embed 版 install.sh 安装后自动触发 login 流程
2. 安装脚本包含同步检查逻辑，确保 embed 脚本与源脚本一致
3. Makefile 构建产物正确嵌入更新后的 install.sh
4. 安装流程在缺少凭证时给出正确引导
