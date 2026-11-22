package engine

const OutcomeHelp Outcome = "help"

const (
	helpCommandsEnglish = "Available commands:\n\n" +
		"- `/help` — Show available commands.\n" +
		"- `/new [message]` — Start a new chat.\n" +
		"- `/clear [message]` — Reset the agent context in this chat; keep its history.\n"

	HelpTextEnglish = helpCommandsEnglish +
		"- `/issue <title>` — Create an issue. Add an optional description on the following lines.\n\n" +
		"In groups or channels, mention the bot before the command."

	HelpTextChinese = "可用命令：\n\n" +
		"- `/help` — 查看命令帮助。\n" +
		"- `/new [消息]` — 新建对话。\n" +
		"- `/clear [消息]` — 重置当前对话的智能体上下文，保留历史记录。\n" +
		"- `/issue <标题>` — 创建任务，后续行可填写描述。\n\n" +
		"群聊中请先 @ 机器人，再输入命令。"

	// Slack's native /issue uses quick-create rather than the message parser.
	SlackHelpText = helpCommandsEnglish +
		"- `/issue <description>` — Ask the agent to create an issue. You'll receive a Multica notification when it's ready.\n\n" +
		"In channels, use `@Multica /new` or `@Multica /clear` in the target thread."
)

// IsHelpCommand recognizes /help using the same first-line and token rules as
// the session commands, without treating help as a context-changing directive.
func IsHelpCommand(body string) bool {
	_, ok := parseLeadingCommand(body, "/help")
	return ok
}
