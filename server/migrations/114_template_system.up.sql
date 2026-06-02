-- 114_template_system.up.sql
-- Dynamic workflow templates: mark workflows as templates, track lineage,
-- and add workflow admin permission bit to users (global, not workspace-scoped).
ALTER TABLE workflow ADD COLUMN IF NOT EXISTS is_template BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE workflow ADD COLUMN IF NOT EXISTS source_template_id UUID REFERENCES workflow(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_workflow_is_template ON workflow(workspace_id, is_template);
CREATE INDEX IF NOT EXISTS idx_workflow_source_template ON workflow(source_template_id);

ALTER TABLE "user" ADD COLUMN IF NOT EXISTS can_manage_workflows BOOLEAN NOT NULL DEFAULT FALSE;

-- Seed one global AI Coding template (cross-workspace, no workspace_id filtering)
DO $$
DECLARE
    tmpl_id UUID;
    n1 UUID; n2 UUID; n3 UUID; n4 UUID; n5 UUID;
    first_ws_id UUID;
BEGIN
    SELECT id INTO first_ws_id FROM workspace LIMIT 1;
    IF first_ws_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM workflow WHERE is_template = TRUE AND title = 'AI Coding 全链路'
    ) THEN
        INSERT INTO workflow (workspace_id, title, description, status, max_retries, created_by_type, created_by_id, is_template)
        VALUES (first_ws_id, 'AI Coding 全链路', '需求分析 → 架构设计 → 任务拆分 → 编码 → 测试，覆盖完整软件开发生命周期。', 'active', 3, 'member', first_ws_id, TRUE)
        RETURNING id INTO tmpl_id;

        INSERT INTO workflow_node (workflow_id, title, description, position_x, position_y, format_schema, worker_type, worker_instructions, critic_type, critic_instructions, sort_order)
        VALUES (tmpl_id, '需求分析', '分析需求并产出需求文档', 100, 50, '{"type":"object","properties":{"idea":{"type":"string","description":"产品构思或需求描述"}},"required":["idea"]}', 'agent', '你是一位资深产品需求分析师。请根据输入的产品构思，撰写一份完整的需求分析文档，包括：功能需求、非功能需求、用户故事、验收标准。', 'human', '作为评审者，请评估需求文档的完整性、清晰度和可行性。检查是否遗漏了关键场景。如果不通过，请明确指出需要改进的部分。', 0)
        RETURNING id INTO n1;

        INSERT INTO workflow_node (workflow_id, title, description, position_x, position_y, format_schema, worker_type, worker_instructions, critic_type, critic_instructions, sort_order)
        VALUES (tmpl_id, '架构设计', '基于需求文档设计技术架构', 350, 50, '{"type":"object","properties":{"requirement_doc":{"type":"string"}},"required":["requirement_doc"]}', 'agent', '你是一位资深技术架构师。请根据需求文档，撰写技术架构设计方案，包括：技术选型、系统架构图描述、模块划分、数据流设计、接口设计原则。', 'human', '作为技术负责人，请评审架构方案的合理性、可扩展性和技术风险。如果不通过，请指出具体问题和改进方向。', 1)
        RETURNING id INTO n2;

        INSERT INTO workflow_node (workflow_id, title, description, position_x, position_y, format_schema, worker_type, worker_instructions, critic_type, critic_instructions, sort_order)
        VALUES (tmpl_id, '任务拆分', '将架构设计拆分为具体开发任务', 600, 50, '{"type":"object","properties":{"architecture_doc":{"type":"string"}},"required":["architecture_doc"]}', 'agent', '你是一位资深项目经理。请根据架构设计文档，将工作拆分为可执行的开发任务，每个任务应包含标题、描述、预估工时和优先级。', 'human', '请评审任务拆分的合理性：粒度是否合适？是否有遗漏？依赖关系是否清晰？', 2)
        RETURNING id INTO n3;

        INSERT INTO workflow_node (workflow_id, title, description, position_x, position_y, format_schema, worker_type, worker_instructions, critic_type, critic_instructions, sort_order)
        VALUES (tmpl_id, '编码', '根据任务拆分进行编码实现', 850, 50, '{"type":"object","properties":{"tasks":{"type":"array"}},"required":["tasks"]}', 'agent', '你是一位资深软件工程师。请根据分配的任务进行编码实现，确保代码质量、测试覆盖和文档完整。', 'agent', '作为代码审查Agent，请检查代码的正确性、安全性、性能、代码风格和测试覆盖。如不通过请指出具体问题。', 3)
        RETURNING id INTO n4;

        INSERT INTO workflow_node (workflow_id, title, description, position_x, position_y, format_schema, worker_type, worker_instructions, critic_type, critic_instructions, sort_order)
        VALUES (tmpl_id, '测试', '对编码结果进行全面测试', 1100, 50, '{"type":"object","properties":{"code_changes":{"type":"array"}},"required":["code_changes"]}', 'agent', '你是一位资深测试工程师。请对代码变更进行全面的测试验证，包括单元测试、集成测试、端到端测试，并产出测试报告。', 'human', '请评审测试报告：测试覆盖率是否充分？是否有遗漏的测试场景？测试结果是否可靠？', 4)
        RETURNING id INTO n5;

        INSERT INTO workflow_edge (workflow_id, source_node_id, target_node_id) VALUES (tmpl_id, n1, n2);
        INSERT INTO workflow_edge (workflow_id, source_node_id, target_node_id) VALUES (tmpl_id, n2, n3);
        INSERT INTO workflow_edge (workflow_id, source_node_id, target_node_id) VALUES (tmpl_id, n3, n4);
        INSERT INTO workflow_edge (workflow_id, source_node_id, target_node_id) VALUES (tmpl_id, n4, n5);
    END IF;
END $$;
