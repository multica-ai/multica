Purpose: Verify that the mobile issue list supports filtering by labels.

Preconditions: The Multica mobile app is running (or H5/responsive mobile view). The user is signed in. The workspace has at least two labels configured. Issues exist that are tagged with different labels.

User flow: Open the Issues list screen on mobile. Locate the filter controls (filter icon, filter bar, or filter drawer). Look for a "Label" / "标签" filter option. Tap to open the label filter selector. Select one specific label. Apply the filter. Verify that only issues with the selected label are displayed in the list. Change the filter to a different label. Verify the list updates to show only issues with the new label. Clear the label filter. Verify all issues are shown again.

Expected results: The label filter option is available in the mobile issue list filter controls. Selecting a label filters the displayed issues to only those tagged with that label. The filter is applied via the API (the `label_ids` parameter is sent in the request). Changing or clearing the filter updates the list accordingly. The filter state is visually indicated (active filter badge or highlighted filter chip). Empty results when no issues match the selected label show an appropriate empty-state message.

Notes for automation: Look for filter controls by icon (funnel/filter icon) or text ("筛选", "Filter"). The label filter may be in a dropdown, drawer, or chip bar. Verify filtering by checking that the displayed issues all have the selected label, or by confirming a reduced count compared to the unfiltered list.
