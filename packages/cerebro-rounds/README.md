# Cerebro Rounds

A Round is a named group of issue conversations. It never reroutes or otherwise
changes the normal inbox and trigger flow.

Pressing Play creates an answer snapshot from member issues that currently have
an unread inbox message and no active task. A scheduled wakeup is not an active
run and therefore does not hide an unread message. The Ready view shows the
unhandled snapshot items, Handled this round shows items answered after that
snapshot began, and All messages always shows the full member list with the
same rows, in the same order, used by the normal inbox. Round members without a
current inbox message do not render a separate issue-title row.

Rounds are listed in the owner's own order. Dragging a round by its grip — in
the inbox block or in the Manage rounds panel — writes that order for the owner,
and a newly created round lands last. Both lists render the same order.

Starting again replaces the active snapshot. Replies continue through their
ordinary trigger path and only add a handled timestamp to the current snapshot.
Pausing ends the active snapshot, folds the Round, and returns its unanswered
items to Ready for the next Play. Handled items stay handled.
