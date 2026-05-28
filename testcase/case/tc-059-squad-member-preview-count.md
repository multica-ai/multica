Purpose: Verify that squad responses include member_count and member_preview fields, and that the squad popover/hover card displays member count and up to 3 member avatars. Also verify Fork's private agent selection restriction in squad creation.

Preconditions: The Multica web app is reachable. A squad exists with at least 4 members (agents and/or humans). The user is signed in as a workspace member.

User flow:
1. Navigate to the Agents or Issues page where squad avatars/badges are visible.
2. Hover over a squad avatar to trigger the squad popover/hover card.
3. Verify the hover card shows: squad name, member count (e.g. "4 members"), and up to 3 member avatar previews.
4. Click through to the squad detail page. Verify the full member list is shown with correct count.
5. (Fork feature) Create or edit a squad. In the agent member picker, verify that only agents the current user owns or public agents are selectable — private agents owned by others are not listed.

Expected results:
- Squad list/card responses include `member_count` (integer) and `member_preview` (array of up to 3 members with member_type, member_id, role).
- The hover card/popover renders the count in a section label (not header badge) and shows preview avatars.
- Private agents owned by other users do not appear in the squad agent picker (Fork restriction).
- The squad detail page shows the complete member list matching the count.

Notes for automation: The hover card is triggered by mouse hover on the squad ActorAvatar component. The member count appears as a label like "N members" within a section, not as a badge in the card header. For the private agent restriction, attempt to search for another user's private agent in the picker — it should not appear.
