-- FIR-3545: the iOS Shortcut now uses the user's normal authenticated PWA
-- session. Remove the abandoned public token intake and its credential table.
DROP TABLE IF EXISTS cerebro_ios_share_inbox;
