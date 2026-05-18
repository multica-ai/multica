Purpose: Verify that the @mention feature in issue comments correctly filters agent suggestions to show only appropriate agents based on ownership rules, and that recent @mention targets are prioritized.

Preconditions: The Multica web app is reachable. The user is signed in. The workspace has multiple agents: some owned by the current user and some by other members. At least one agent has been recently @mentioned by the current user.

User flow: Open an issue and click into the comment input. Type `@` to trigger the mention suggestion dropdown. Observe the agent list shown. Verify that the list shows workspace agents visible to the current user. Check that recently @mentioned agents appear near the top of the list. Select an agent and submit the comment. The comment should trigger the agent's task execution.

Expected results: The @mention dropdown shows agents that the current user can invoke (workspace agents visible per OPE-424/495 rules). Private agents owned by other users do not appear in the mention list unless the current user has access. Recently @mentioned agents are prioritized (appear first in the list). After submitting a comment with an @mention, the mentioned agent receives a task trigger. The @mention renders as a clickable link in the submitted comment.

Notes for automation: Trigger the mention dropdown by typing `@` in the comment input. The dropdown is a suggestion list that can be navigated with arrow keys. Check the order of suggestions — recently used agents should appear before others.
