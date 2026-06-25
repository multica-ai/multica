核心需求：在单个Issue详情页中展示其关联Workflow的动态执行状态，包括节点进展、阻塞、产出等信息（未关联workflow的issue点击进入原详情页），同时隐藏AI自动生成的子Issue
用户旅程确认：用户进入Issue列表（仅显示手动创建的Issue，筛除自动生成的），选择一个Issue点开，进入该Issue的详情页，详情页展示与该Issue关联的Workflow动态图（参考workflow-panorama-page，新增更多信息展示），图中包含所有节点及其状态（如完成、阻塞、进行中），点击节点可查看详情。
节点信息展示：每个节点应显示状态、产出物、所有角色（如：Worker和Critic）的进展等详细信息。