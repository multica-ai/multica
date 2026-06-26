package main

import (
	"github.com/spf13/cobra"
)

var notificationCmd = &cobra.Command{
	Use:   "notification",
	Short: "Query notification status for the current agent or squad",
	Long: `Pull-side notification queries complement the push-based
notification bus. Agents and humans can check what notifications
are pending for them, which issues are waiting for their action,
and acknowledge notifications.`,
	Aliases: []string{"notif"},
}

var notificationPendingCmd = &cobra.Command{
	Use:   "pending",
	Short: "List pending notifications for the current agent/squad",
	RunE:  runNotificationPending,
}

var notificationWaitingForMeCmd = &cobra.Command{
	Use:   "waiting-for-me",
	Short: "List issues waiting for the current agent's action",
	RunE:  runNotificationWaitingForMe,
}

var notificationAcknowledgeCmd = &cobra.Command{
	Use:   "acknowledge <notification-id>",
	Short: "Acknowledge a notification (mark as read)",
	Args:  exactArgs(1),
	RunE:  runNotificationAcknowledge,
}

func init() {
	notificationCmd.GroupID = groupCore

	notificationCmd.AddCommand(notificationPendingCmd)
	notificationCmd.AddCommand(notificationWaitingForMeCmd)
	notificationCmd.AddCommand(notificationAcknowledgeCmd)

	notificationPendingCmd.Flags().String("output", "table", "Output format (table/json)")

	rootCmd.AddCommand(notificationCmd)
}

func runNotificationPending(cmd *cobra.Command, args []string) error {
	// Placeholder — notification records table TBD
	cmd.Println("⚠️  Notification query will be available when the notification_records table is created.")
	cmd.Println("The notification engine (rules R1-R5 + detectors D1-D6) is already active for push-based events.")
	return nil
}

func runNotificationWaitingForMe(cmd *cobra.Command, args []string) error {
	cmd.Println("⚠️  'multica notification waiting-for-me' requires the notification_records table.")
	cmd.Println("Use 'multica issue list --status blocked' to find issues needing action.")
	return nil
}

func runNotificationAcknowledge(cmd *cobra.Command, args []string) error {
	cmd.Println("⚠️  Notification acknowledgement requires the notification_records table.")
	return nil
}
