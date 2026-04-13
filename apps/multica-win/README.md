# Multica Win

**Professional Windows Control Center for Multica**

Multica Win is a lightweight, high-performance desktop management panel built with **Tauri 2.0**, **React 19**, and **Tailwind CSS 4**. It provides a native Windows experience for monitoring and controlling your Multica decentralized AI mesh.

## 🚀 Key Features

- **💎 Modern Dashboard**: A streamlined, professional interface designed for high-density information display without visual clutter.
- **🛡️ Secure Daemon Management**: Integrated lifecycle management for the Multica daemon. Safe shutdown sequences ensure no orphan processes are left behind.
- **🖥️ DPI-Immune Layout**: Advanced "Gen-2" responsive architecture that remains pixel-perfect regardless of Windows "Text Scaling" (DPI) settings.
- **📂 One-Click Explorer**: Direct integration with Windows File Explorer. Quickly access your global config (`~/.multica`) or specific local workspace directories.
- **🔌 Onboarding & Setup**: Built-in connection wizard for both cloud-mesh users and private server deployments.
- **📥 System Tray Integration**: Compact tray icon with a quick-action menu for seamless background operation.

## 🛠️ Tech Stack

- **Backend**: Rust (Tauri 2.0)
- **Frontend**: React 19 (TypeScript)
- **Styling**: Tailwind CSS v4.0 (Performance-first CSS engine)
- **Data Flow**: TanStack Query v5 (High-performance caching and sync)
- **Icons**: Lucide React

## 📦 Getting Started

### Prerequisites

- **Rust**: [rustup.rs](https://rustup.rs/)
- **Node.js**: v18+ (pnpm recommended)
- **WebView2**: Standard on Windows 11 and updated Windows 10.

### Development

```bash
# Install dependencies
pnpm install

# Run in development mode
pnpm tauri dev
```

### Build

```bash
# Generate production-ready standalone EXE and Setup installer
pnpm tauri build
```

The output will be located in `src-tauri/target/release/`.

---

# Multica Win (中文说明)

**专业的 Multica Windows 控制中心**

Multica Win 是基于 **Tauri 2.0**、**React 19** 和 **Tailwind CSS 4** 构建的轻量级、高性能桌面管理面板。它为管理您的 Multica 分布式 AI 网格提供了原生 Windows 体验。

## 🚀 核心特性

- **💎 现代仪表盘**：专为高密度信息展示设计的流线型界面，保持专业感的同时拒绝视觉拥挤。
- **🛡️ 安全守护进程管理**：内置 Multica Daemon 的生命周期管理。完善的关闭序列确保 100% 杀死后台进程，不留隐患。
- **🖥️ 防碎裂布局**：采用第二代响应式架构，完美适配 Windows “文本放大”系统设置，在任何 DPI 缩放比例下均不乱码、不位移。
- **📂 一键直达资源管理器**：深度打通 Windows Explorer。一键访问全局配置目录 (`~/.multica`) 或特定的本地工作区物理目录。
- **🔌 连接引导**：内置连接向导，支持公有云网格及私有部署服务器的手动配置与健康测试。
- **📥 系统托盘集成**：精简的托盘图标和右键菜单，支持最小化到后台静默运行。

## 📦 构建与发布

1. 安装 Rust 环境与 Node.js (pnpm)。
2. 执行 `pnpm install` 安装依赖。
3. 执行 `pnpm tauri build` 生成独立可执行文件 (.exe) 和安装包。

产物位于 `src-tauri/target/release/` 目录下。
