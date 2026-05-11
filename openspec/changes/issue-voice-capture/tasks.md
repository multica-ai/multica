## 1. OpenSpec 与产品对齐

- [ ] 1.1 创建并对齐本 change 的 proposal / design / spec / tasks 工件。
- [ ] 1.2 补充独立 PRD，明确用户场景、成功指标、阶段目标与降级策略。

## 2. Phase 1：浏览器原生语音录入

- [ ] 2.1 在 create issue 流程中增加语音输入入口和权限提示。
- [ ] 2.2 实现浏览器能力探测，区分“录音 + 转录”“仅录音”“不支持”三类状态。
- [ ] 2.3 实现 transcript 到 title / description 的映射规则与 review 交互。
- [ ] 2.4 在 issue 创建成功后，按用户选择上传原始录音为 issue attachment。
- [ ] 2.5 补充前端单元测试与组件测试。

## 3. Phase 2：服务端转录增强

- [ ] 3.1 设计独立的 transcription provider 配置，不与现有 chat completion 配置混用。
- [ ] 3.2 增加服务端转录接口与必要的 provider capability 判定。
- [ ] 3.3 在前端增加“浏览器原生转录不可用时走服务端转录”的降级路径。
- [ ] 3.4 补充后端测试、前端集成测试和错误处理验证。

## 4. 验证

- [ ] 4.1 补充 E2E，验证语音录入 happy path 和关键降级路径。
- [ ] 4.2 记录浏览器兼容性结论和已知限制。
- [ ] 4.3 运行相关前端、后端、E2E 验证并记录证据。